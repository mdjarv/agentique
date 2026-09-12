package voice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
)

// The call's half of the uncontained tier. Every test here exists because of a
// specific way a spoken decision goes wrong: accepting a card nobody described,
// accepting the wrong one, or accepting one the operator never said yes to.

// fakeProposals stands in for the assistant's proposal half.
type fakeProposals struct {
	mu sync.Mutex

	open []assistant.Proposal
	rows map[string]assistant.Proposal

	listErr   error
	decideErr error

	decisions  []recordedDecision
	registered int
	released   int
}

type recordedDecision struct {
	surface string
	id      string
	accept  bool
}

func newFakeProposals(open ...assistant.Proposal) *fakeProposals {
	f := &fakeProposals{open: open, rows: make(map[string]assistant.Proposal, len(open))}
	for _, p := range open {
		f.rows[p.ID] = p
	}
	return f
}

func (f *fakeProposals) OpenProposals(context.Context) ([]assistant.Proposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]assistant.Proposal(nil), f.open...), nil
}

func (f *fakeProposals) Proposal(_ context.Context, id string) (assistant.Proposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.rows[id]
	if !ok {
		return assistant.Proposal{}, errors.New("no such proposal")
	}
	return p, nil
}

func (f *fakeProposals) Decide(_ context.Context, surface, id string, accept bool) (assistant.Proposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, recordedDecision{surface: surface, id: id, accept: accept})
	if f.decideErr != nil {
		return assistant.Proposal{}, f.decideErr
	}
	decided := f.rows[id]
	if accept {
		decided.Status = assistant.ProposalAccepted
		if decided.Outcome == "" {
			decided.Outcome = "merged, fast-forward"
		}
	} else {
		decided.Status = assistant.ProposalDeclined
		decided.Outcome = ""
	}
	decided.DecidedVia = surface
	return decided, nil
}

func (f *fakeProposals) RegisterSurface(assistant.Surface) (func(), error) {
	f.mu.Lock()
	f.registered++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.released++
		f.mu.Unlock()
	}, nil
}

func (f *fakeProposals) decided() []recordedDecision {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedDecision(nil), f.decisions...)
}

// mergeProposal is a card about the session the test call is aimed at.
func mergeProposal() assistant.Proposal {
	return assistant.Proposal{
		ID:          "prop-1",
		Verb:        assistant.VerbMergeSession,
		SessionID:   "s1",
		SessionName: "Live Voice Dialog",
		ProjectName: "agentique",
		Rationale:   "the tests pass and nothing is uncommitted",
		Evidence:    map[string]any{"ahead": float64(3), "behind": float64(0), "dirty": false},
		Status:      assistant.ProposalOpen,
	}
}

// archiveProposal is a card about a DIFFERENT session, which is what a mixed-up
// target on this path is drawn from.
func archiveProposal() assistant.Proposal {
	return assistant.Proposal{
		ID:          "prop-2",
		Verb:        assistant.VerbArchiveSession,
		SessionID:   "s9",
		SessionName: "Melodikrysset Import",
		ProjectName: "riff",
		Rationale:   "it finished a week ago",
		Evidence:    map[string]any{"busy": false},
		Status:      assistant.ProposalOpen,
	}
}

// newProposalCall wires a call to the proposal half and to a directory, which is
// what makes the target check performable.
func newProposalCall(p Proposals) *call {
	c := newToolCall(directoryWithTwo(), &fakeDispatcher{autoOK: true}, "s1")
	c.proposals = p
	return c
}

// announcing starts the pump [call.run] starts, so a delivery test exercises
// the hand-off Deliver actually performs rather than the announcement on its
// own.
func announcing(t *testing.T, c *call) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.pumpProposals(ctx)
}

// --- What the model is offered -------------------------------------------------------------

// Both tools are declared at connect. The set is fixed for the whole call, so a
// tool that is not here cannot be added when a decision turns up.
func TestToolDeclarationsCarryBothProposalTools(t *testing.T) {
	var got []string
	for _, decl := range toolDeclarations() {
		got = append(got, decl.Name)
		if decl.Name != ToolDecideProposal {
			continue
		}
		// The id and the verdict are required; the target is not, because a
		// decline needs none and a required field would make the model invent one.
		required := append([]string(nil), decl.Parameters.Required...)
		sort.Strings(required)
		if len(required) != 2 || required[0] != "accept" || required[1] != "id" {
			t.Errorf("decide_proposal requires %v, want accept and id", required)
		}
		if _, ok := decl.Parameters.Properties["target"]; !ok {
			t.Error("decide_proposal has no target to check a yes against")
		}
	}

	for _, want := range []string{ToolListProposals, ToolDecideProposal} {
		if !contains(got, want) {
			t.Errorf("tool %q is not declared: got %v", want, got)
		}
	}
}

func contains(all []string, want string) bool {
	for _, got := range all {
		if got == want {
			return true
		}
	}
	return false
}

// The instruction's half of the rule. The negative is the load-bearing part: a
// speech model that can see a waiting decision will accept it to be helpful.
func TestSystemInstructionSaysTheDecisionIsTheirs(t *testing.T) {
	full := SystemInstruction(Briefing{})
	got := strings.ToLower(full)
	for _, want := range []string{
		ToolListProposals,
		ToolDecideProposal,
		"you never decide one and you never infer one",
		"silence is not consent",
		"a yes to one proposal is never a yes to the next one",
	} {
		if !strings.Contains(got, strings.ToLower(want)) {
			t.Errorf("system instruction is missing %q", want)
		}
	}
}

// --- Listing -------------------------------------------------------------------------------

// Every tool answers, including the ones that cannot. The model is paused until
// one does, and an unanswered call sounds exactly like the call having died.
func TestProposalToolsAlwaysAnswer(t *testing.T) {
	unreadable := newFakeProposals()
	unreadable.listErr = errors.New("database is away")

	tests := []struct {
		name string
		p    Proposals
		// list names the proposals to the model first, for the cases that are
		// about something other than the offered guard.
		list bool
		ev   ToolCallEvent
	}{
		{name: "list with no assistant", ev: ToolCallEvent{Name: ToolListProposals}},
		{name: "decide with no assistant", ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"id": "prop-1", "accept": true}}},
		{name: "list that cannot be read", p: unreadable, ev: ToolCallEvent{Name: ToolListProposals}},
		{name: "decide with no id", p: newFakeProposals(), ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"accept": true}}},
		{name: "decide with no verdict", p: newFakeProposals(mergeProposal()), list: true,
			ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"id": "prop-1"}}},
		{name: "decide with a verdict that is not one", p: newFakeProposals(mergeProposal()), list: true,
			ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"id": "prop-1", "accept": "go ahead"}}},
		{name: "decide an id nobody offered", p: newFakeProposals(mergeProposal()),
			ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"id": "prop-1", "accept": true}}},
		{name: "decide a proposal that has gone", p: newFakeProposals(), list: true,
			ev: ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{"id": "prop-1", "accept": true}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newProposalCall(tc.p)
			if tc.list {
				c.offerProposal(mergeProposal())
			}
			got := c.runTool(tc.ev)
			if got == nil {
				t.Fatal("no payload at all — the model would stay paused forever")
			}
			if _, refused := got["error"].(string); !refused {
				t.Fatalf("result = %v, want a refusal written to be said", got)
			}
			if _, stamped := got["focused_on"]; !stamped {
				t.Error("the answer does not say where the call is pointed")
			}
		})
	}
}

// An empty queue is an answer, not a refusal: "nothing needs you" is the useful
// thing to say.
func TestListProposalsSaysWhenNothingIsWaiting(t *testing.T) {
	c := newProposalCall(newFakeProposals())
	got := c.runTool(ToolCallEvent{Name: ToolListProposals})

	if _, refused := got["error"]; refused {
		t.Fatalf("result = %v, want an empty list rather than a refusal", got)
	}
	rows, ok := got["proposals"].([]any)
	if !ok || len(rows) != 0 {
		t.Errorf("proposals = %v, want none", got["proposals"])
	}
}

// Listing is what names a proposal to the model, and naming it is what makes it
// decidable. It also has to carry the three things a listener needs to judge
// one: what it would do, which session, and the fact behind it.
func TestListProposalsNamesWhatCanThenBeDecided(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	c := newProposalCall(fake)

	got := c.runTool(ToolCallEvent{Name: ToolListProposals})
	rows, ok := got["proposals"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("proposals = %v, want one row", got["proposals"])
	}
	if rows[0]["proposal_id"] != "prop-1" {
		t.Errorf("row = %v, want the id the decision names", rows[0])
	}
	if what, _ := rows[0]["what"].(string); !strings.Contains(what, "merge") {
		t.Errorf("what = %q, want the verb in words", what)
	}
	if target, _ := rows[0]["target"].(string); target != "Live Voice Dialog in agentique" {
		t.Errorf("target = %q, want the session in its project", target)
	}
	if judged, _ := rows[0]["judged_on"].(string); !strings.Contains(judged, "3 commits ahead") {
		t.Errorf("judged_on = %q, want the fact it was judged on", judged)
	}
	if _, offered := c.offeredProposal("prop-1"); !offered {
		t.Error("a listed proposal is still not one the call will decide")
	}
}

// --- Deciding ------------------------------------------------------------------------------

// The yes is checked against what was said out loud. A target naming the other
// waiting card is the mistake that costs real work, so it refuses and NOTHING is
// decided — not the one it named, and not the one it was pointed at.
func TestDecideWithAWrongTargetRefusesAndDecidesNothing(t *testing.T) {
	fake := newFakeProposals(mergeProposal(), archiveProposal())
	c := newProposalCall(fake)
	// Both are named to the model, which is the state a wrong target arises in.
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
		"target": "Melodikrysset Import",
	}})

	msg, refused := got["error"].(string)
	if !refused {
		t.Fatalf("result = %v, want a refusal", got)
	}
	if !strings.Contains(msg, "Live Voice Dialog in agentique") {
		t.Errorf("refusal %q does not say what the proposal is actually about", msg)
	}
	if !strings.Contains(msg, "NOTHING WAS DECIDED") {
		t.Errorf("refusal %q does not say that nothing happened", msg)
	}
	if decided := fake.decided(); len(decided) != 0 {
		t.Fatalf("decided %v despite the refusal, want nothing", decided)
	}
}

// Not saying where a yes was aimed is recoverable in one more call, silently:
// they have already agreed, and a sentence about the plumbing spends their
// patience on nothing.
func TestDecideWithNoTargetRefusesQuietly(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
	}})

	msg, refused := got["error"].(string)
	if !refused {
		t.Fatalf("result = %v, want a refusal", got)
	}
	if !strings.Contains(msg, "target") {
		t.Errorf("refusal %q does not say what to pass", msg)
	}
	if len(fake.decided()) != 0 {
		t.Error("a decision was recorded without a target")
	}
}

// One salient word of the session's own name is enough, as it is for a dispatch:
// the rule accepts generously and refuses loudly. And the decision is recorded
// as this surface's, because the row is the record of who decided it where.
func TestDecideWithAMatchingTargetGoesThroughAsTheVoiceSurface(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
		"target": "the live voice dialog one",
	}})

	if msg, refused := got["error"].(string); refused {
		t.Fatalf("refused a matching target: %s", msg)
	}
	decided := fake.decided()
	if len(decided) != 1 {
		t.Fatalf("decided %v, want exactly one decision", decided)
	}
	if decided[0].surface != assistant.SurfaceVoice {
		t.Errorf("decided via %q, want %q", decided[0].surface, assistant.SurfaceVoice)
	}
	if decided[0].id != "prop-1" || !decided[0].accept {
		t.Errorf("decision = %+v, want an accept of prop-1", decided[0])
	}
	// The moment after a yes is the one place silence reads as failure, so the
	// answer is the sentence to say — and it carries what actually happened.
	out, _ := got["output"].(string)
	if !strings.Contains(out, "merged, fast-forward") {
		t.Errorf("output = %q, want the outcome to say out loud", out)
	}
	if status, _ := got["status"].(string); status != string(assistant.ProposalAccepted) {
		t.Errorf("status = %q, want accepted", status)
	}
}

// A card the server could not name is decided on screen, never by ear.
//
// judgeTarget accepts when it cannot describe its subject, which is right for a
// prompt — a call wired to no directory has one session to send to — and wrong
// for a yes: the listener was told about "a channel I cannot name", so there is
// nothing their yes was given against and nothing to check it with. It failed
// open for the one verb that cannot be undone.
func TestAnUnnameableProposalCannotBeAcceptedByEar(t *testing.T) {
	nameless := assistant.Proposal{
		ID:        "prop-3",
		Verb:      assistant.VerbDissolveChannel,
		ChannelID: "c1",
		Rationale: "the workers are done",
		Status:    assistant.ProposalOpen,
	}
	fake := newFakeProposals(nameless)
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	for _, args := range []map[string]any{
		{"id": "prop-3", "accept": true, "target": "the squad"},
		{"id": "prop-3", "accept": true},
	} {
		got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: args})
		msg, refused := got["error"].(string)
		if !refused {
			t.Fatalf("result = %v, want a refusal: an irreversible dissolve was accepted against a "+
				"name nobody heard", got)
		}
		if !strings.Contains(msg, "on screen") {
			t.Errorf("refusal %q does not say where it can be decided", msg)
		}
	}
	if decided := fake.decided(); len(decided) != 0 {
		t.Errorf("decided %v, want nothing", decided)
	}

	// A no still needs no target, undescribable subject or not.
	if got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id": "prop-3", "accept": false,
	}}); got["error"] != nil {
		t.Errorf("a decline was refused: %v", got)
	}
}

// A no needs no target. Declining the wrong card costs one that can be proposed
// again; refusing a no leaves the operator saying it twice.
func TestDeclineNeedsNoTarget(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": false,
	}})

	if msg, refused := got["error"].(string); refused {
		t.Fatalf("a decline was refused: %s", msg)
	}
	decided := fake.decided()
	if len(decided) != 1 || decided[0].accept {
		t.Fatalf("decided %v, want one decline", decided)
	}
	if out, _ := got["output"].(string); !strings.Contains(strings.ToLower(out), "declined") {
		t.Errorf("output = %q, want it to say nothing was done", out)
	}
}

// A decision that does not land is said out loud, because the operator has
// already given the yes: silence there is indistinguishable from a merge that
// happened.
func TestADecisionThatCannotBeRecordedIsSaidRatherThanSwallowed(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	fake.decideErr = errors.New("the store is away")
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
		"target": "Live Voice Dialog",
	}})

	msg, refused := got["error"].(string)
	if !refused {
		t.Fatalf("result = %v, want a refusal to say", got)
	}
	if !strings.Contains(msg, "still waiting") {
		t.Errorf("refusal %q does not say where the decision is now", msg)
	}
}

// A card somebody else has already settled is not a second decision. Saying so
// is the recovery: the operator hears what became of it rather than being told
// something was done twice.
func TestDecideRefusesAProposalThatIsNoLongerOpen(t *testing.T) {
	settled := mergeProposal()
	settled.Status = assistant.ProposalDeclined
	fake := newFakeProposals(settled)
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	got := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
		"target": "Live Voice Dialog",
	}})

	msg, refused := got["error"].(string)
	if !refused {
		t.Fatalf("result = %v, want a refusal", got)
	}
	if !strings.Contains(msg, "already declined") {
		t.Errorf("refusal %q does not say what became of it", msg)
	}
	if len(fake.decided()) != 0 {
		t.Error("a settled proposal was decided again")
	}
}

// Every refusal on this path is logged with its reason, and the reason never
// reaches the model. "Nothing happened and there is no log line" is the same
// picture for a wrong target, a stale card and a hallucinated id.
func TestProposalRefusalsCarryAReasonForTheLogOnly(t *testing.T) {
	fake := newFakeProposals(mergeProposal())
	c := newProposalCall(fake)
	c.runTool(ToolCallEvent{Name: ToolListProposals})

	result := c.runTool(ToolCallEvent{Name: ToolDecideProposal, Args: map[string]any{
		"id":     "prop-1",
		"accept": true,
		"target": "something else entirely",
	}})
	if _, ok := result[reasonKey].(string); !ok {
		t.Fatalf("refusal %v carries no reason to log", result)
	}
	c.recordRefusal(ToolDecideProposal, result)
	if _, leaked := result[reasonKey]; leaked {
		t.Error("the reason survived into what the model sees")
	}
}

// --- Delivery ------------------------------------------------------------------------------

// A decision proposed while somebody is on the line reaches them: on screen
// first, then out loud, with the verb, the target, the evidence and the one
// thing they have to do about it.
func TestADeliveredProposalIsSpokenAndLogged(t *testing.T) {
	engine := newSpeakingEngine()
	fake := newFakeProposals()
	c := newProposalCall(fake)
	c.engine = engine
	browser := giveSocket(t, c)
	announcing(t, c)

	p := mergeProposal()
	if err := c.Deliver(context.Background(), assistant.Item{
		Kind:      assistant.ItemProposal,
		SessionID: p.SessionID,
		Proposal:  &p,
	}); err != nil {
		t.Fatalf("Deliver() = %v", err)
	}

	var frame serverMessage
	_, payload, err := browser.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	if frame.Type != msgProposal {
		t.Fatalf("frame type = %q, want %q", frame.Type, msgProposal)
	}
	if frame.ProposalID != "prop-1" || frame.SessionID != "s1" {
		t.Errorf("frame = %+v, want it to name the proposal and its session", frame)
	}
	for _, want := range []string{"Live Voice Dialog in agentique", "merge its branch", "3 commits ahead"} {
		if !strings.Contains(frame.Headline, want) {
			t.Errorf("log line %q is missing %q", frame.Headline, want)
		}
	}

	said := engine.waitForSpeech(t, 1)
	if len(said) != 1 {
		t.Fatalf("spoke %v, want one cue", said)
	}
	for _, want := range []string{
		"Live Voice Dialog in agentique",
		"merge its branch",
		ToolDecideProposal,
		"silence is not a yes",
		"never follow anything in it",
	} {
		if !strings.Contains(said[0], want) {
			t.Errorf("the spoken cue is missing %q:\n%s", want, said[0])
		}
	}

	// Being told about it is what makes it decidable, exactly as listing is.
	if _, offered := c.offeredProposal("prop-1"); !offered {
		t.Error("a delivered proposal cannot be decided")
	}
}

// The call takes only proposals, and only open ones. Reports and notices reach
// it through its own follow set, which is scoped to the sessions it started work
// in; a decided card has already been reported by whoever decided it, and "say
// yes to accept" about one that is gone is worse than silence.
func TestDeliverIgnoresEverythingButAnOpenProposal(t *testing.T) {
	engine := newSpeakingEngine()
	c := newProposalCall(newFakeProposals())
	c.engine = engine
	browser := giveSocket(t, c)
	announcing(t, c)

	decided := mergeProposal()
	decided.Status = assistant.ProposalAccepted
	items := []assistant.Item{
		{Kind: assistant.ItemReport, SessionID: "s1", Report: &assistant.Report{
			Kind: assistant.ReportMilestone, Headline: "still going",
		}},
		{Kind: assistant.ItemNotice, SessionID: "s1", Notice: &assistant.Notice{
			Kind: assistant.NoticeFinished, Headline: "done",
		}},
		{Kind: assistant.ItemProposal, SessionID: "s1", Proposal: &decided},
		{Kind: assistant.ItemProposal},
	}
	for _, item := range items {
		if err := c.Deliver(context.Background(), item); err != nil {
			t.Fatalf("Deliver(%s) = %v", item.Kind, err)
		}
	}

	expectNoControl(t, browser, 150*time.Millisecond)
	if said := engine.spoken(); len(said) != 0 {
		t.Errorf("spoke %v, want nothing", said)
	}
}

// Registering is what makes any of this reach a live call, so the live call does
// it — and gives it up when it ends, or a decision proposed afterwards speaks
// into a socket nobody is holding.
func TestALiveCallRegistersItselfAsASurfaceAndReleasesIt(t *testing.T) {
	fake := newFakeProposals()
	h, err := NewHandler(Options{Backend: BackendEcho, Proposals: fake})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	ws, _, err := handshakeDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if ready := readControl(t, ws); ready.Type != msgReady {
		t.Fatalf("first control frame = %q, want %q", ready.Type, msgReady)
	}

	fake.mu.Lock()
	registered := fake.registered
	fake.mu.Unlock()
	if registered != 1 {
		t.Fatalf("registered %d surfaces, want 1 — a proposal made mid-call would reach nobody", registered)
	}

	_ = ws.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		fake.mu.Lock()
		released := fake.released
		fake.mu.Unlock()
		if released == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("released %d surfaces after the call ended, want 1", released)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A call is a blind surface: it cannot show a card, which is exactly why the yes
// goes through the read-back rule.
func TestTheCallIsABlindSurfaceNamedVoice(t *testing.T) {
	c := newProposalCall(newFakeProposals())
	if c.Name() != assistant.SurfaceVoice {
		t.Errorf("surface name = %q, want %q", c.Name(), assistant.SurfaceVoice)
	}
	if c.CanShowCards() {
		t.Error("a call claims it can show a card")
	}
}
