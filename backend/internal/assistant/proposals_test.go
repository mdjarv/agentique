package assistant

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
)

// proposalWorld builds a service with one local session, one channel and an
// [Actions] that allows everything, which is the state a test narrows from.
func proposalWorld(t *testing.T, opts ...Option) (*Service, *fakeActions, *fakeDirectory) {
	t.Helper()
	dir := &fakeDirectory{sessions: []SessionRow{{
		ID: "s1", Name: "the retry fix", ProjectName: "riff", ProjectID: "p1", Branch: "agentique/s1",
	}}}
	actions := newFakeActions()
	opts = append([]Option{WithDirectory(dir), WithActions(actions)}, opts...)
	svc, _, _ := newTestService(t, opts...)
	return svc, actions, dir
}

// proposalArgs is a valid ask for one verb: the target, a rationale, and
// whatever else that verb requires.
func proposalArgs(verb string) map[string]any {
	args := map[string]any{"rationale": "the branch is done and the tests pass"}
	switch verb {
	case VerbDissolveChannel:
		args["channel_id"] = "c1"
	case VerbSetSessionModel:
		args["session_id"] = "s1"
		args["model"] = "sonnet"
	case VerbSetSessionMode:
		args["session_id"] = "s1"
		args["mode"] = PermissionModePlan
	default:
		args["session_id"] = "s1"
	}
	return args
}

// Every uncontained verb writes a card and performs nothing. That is the whole
// tier: the handler's only power is a row.
func TestEveryUncontainedVerbProposes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)
	// Behind, so the merge verb has something to rebase and the rebase verb is
	// allowed; the merge case has its own test.
	actions.branch = BranchFacts{Ahead: 2, Behind: 1, MergeStatus: "clean"}

	for _, verb := range svc.Verbs() {
		if verb.Tier != TierUncontained {
			continue
		}
		if verb.Name == VerbMergeSession {
			// A branch that is behind cannot merge, which is the next test.
			continue
		}
		payload, err := svc.Invoke(ctx, verb.Name, proposalArgs(verb.Name))
		if err != nil {
			t.Fatalf("Invoke(%q) = %v", verb.Name, err)
		}
		if refusal, refused := payload["error"]; refused {
			t.Fatalf("Invoke(%q) refused: %v", verb.Name, refusal)
		}
		id, _ := payload["proposal_id"].(string)
		if id == "" {
			t.Fatalf("Invoke(%q) = %v, want a proposal id", verb.Name, payload)
		}
		note, _ := payload["note"].(string)
		if !strings.Contains(note, "PROPOSED, NOT DONE") {
			t.Errorf("Invoke(%q) does not tell the head it is not done: %q", verb.Name, note)
		}

		p, err := svc.Proposal(ctx, id)
		if err != nil {
			t.Fatalf("Proposal(%q) = %v", id, err)
		}
		if p.Status != ProposalOpen {
			t.Errorf("%s proposal is %q, want open", verb.Name, p.Status)
		}
		if p.Rationale == "" {
			t.Errorf("%s proposal has no rationale", verb.Name)
		}
		if len(p.Evidence) == 0 {
			t.Errorf("%s proposal carries no evidence, so nothing was judged", verb.Name)
		}
		if p.ExpiresAt == "" {
			t.Errorf("%s proposal never expires", verb.Name)
		}
	}

	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("proposing performed %v — the tier is the whole containment story", performed)
	}
}

// A rationale is required: the card is read by somebody who was not in the
// conversation.
func TestAProposalNeedsARationale(t *testing.T) {
	t.Parallel()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(context.Background(), VerbArchiveSession, map[string]any{"session_id": "s1"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if _, refused := payload["error"]; !refused {
		t.Fatalf("payload = %v, want a refusal", payload)
	}
	if actions.reads("busy") != 0 {
		t.Error("a reasonless ask still read the facts")
	}
}

// The refusal the contract names by hand. A merge of a branch that is behind
// is "rebase first", not a card: the server's merge is fast-forward only, so
// the proposal could only ever come back needs_rebase.
func TestAMergeOfABehindBranchRefusesRatherThanProposing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)
	actions.branch = BranchFacts{Ahead: 2, Behind: 3, MergeStatus: "clean"}

	payload, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	refusal, refused := payload["error"].(string)
	if !refused {
		t.Fatalf("payload = %v, want a refusal rather than a proposal", payload)
	}
	if !strings.Contains(refusal, "rebase") {
		t.Errorf("the refusal does not name the other verb: %q", refusal)
	}
	if _, leaked := payload["proposal_id"]; leaked {
		t.Error("a refused merge still wrote a card")
	}

	open, err := svc.OpenProposals(ctx)
	if err != nil {
		t.Fatalf("OpenProposals() = %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("%d proposals were written for something the facts already ruled out", len(open))
	}
}

// Asking twice for the same thing is one card. Two rows would be two buttons
// for one decision, and accepting either would leave the other pointing at
// work already done.
func TestAskingTwiceReturnsTheOpenProposal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	first, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	second, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}

	if first["proposal_id"] != second["proposal_id"] {
		t.Errorf("a second ask made a second card: %v then %v", first, second)
	}
	if already, _ := second["already_open"].(bool); !already {
		t.Errorf("the second answer does not say it is already waiting: %v", second)
	}
	// The short circuit is also the point: re-reading the facts for a duplicate
	// is five git subprocesses nobody asked for.
	if reads := actions.reads("branch"); reads != 1 {
		t.Errorf("the facts were read %d times for two asks about one thing", reads)
	}

	open, err := svc.OpenProposals(ctx)
	if err != nil {
		t.Fatalf("OpenProposals() = %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("%d open proposals, want 1", len(open))
	}
}

// Accepting performs the action through [Actions] and records what it did.
func TestAcceptPerformsTheAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id, _ := payload["proposal_id"].(string)

	decided, err := svc.Decide(ctx, SurfaceThread, id, true)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalAccepted {
		t.Fatalf("status = %q, want accepted (outcome %q)", decided.Status, decided.Outcome)
	}
	if decided.Outcome == "" {
		t.Error("an accepted proposal says nothing about what happened")
	}
	if decided.DecidedVia != SurfaceThread {
		t.Errorf("decidedVia = %q, want the surface the yes was given on", decided.DecidedVia)
	}
	if performed := actions.performed(); len(performed) != 1 || performed[0] != "merge" {
		t.Fatalf("performed %v, want one merge", performed)
	}
	// Re-read, because the row is the record.
	stored, err := svc.Proposal(ctx, id)
	if err != nil {
		t.Fatalf("Proposal() = %v", err)
	}
	if stored.Status != ProposalAccepted || stored.Outcome != decided.Outcome {
		t.Errorf("the row does not carry the decision: %+v", stored)
	}
	if entries := journalKindsIn(t, svc, JournalProposalDecided); entries == 0 {
		t.Error("the decision was not journaled")
	}
}

// The accept-time re-check is the storage page's rule one layer up: a stale
// card narrows what happens and never widens it.
func TestAcceptOnChangedFactsGoesStaleAndPerformsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id, _ := payload["proposal_id"].(string)

	// The project's branch moved on between the card and the yes.
	actions.branch = BranchFacts{Ahead: 2, Behind: 4, MergeStatus: "clean"}

	decided, err := svc.Decide(ctx, SurfaceThread, id, true)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalStale {
		t.Fatalf("status = %q, want stale", decided.Status)
	}
	if !strings.Contains(decided.Outcome, "rebase") {
		t.Errorf("outcome = %q, want the reason it went stale", decided.Outcome)
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("a stale accept performed %v", performed)
	}
	// The evidence the card was judged on is not rewritten: the comparison is
	// the whole reason the column exists.
	if behind, _ := decided.Evidence["behind"].(float64); behind != 0 {
		t.Errorf("evidence was overwritten at accept: %v", decided.Evidence)
	}
}

// A git operation that refuses is `failed` with its own word as the outcome,
// never a crash.
func TestAFailedExecutorIsAFailedProposal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)
	actions.execErr = &OutcomeError{
		Outcome: "conflict -- it would conflict in 2 files, and the merge was undone",
		Detail:  "a.go, b.go",
	}

	payload, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	decided, err := svc.Decide(ctx, SurfaceThread, payload["proposal_id"].(string), true)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalFailed {
		t.Fatalf("status = %q, want failed", decided.Status)
	}
	if !strings.Contains(decided.Outcome, "conflict") {
		t.Errorf("outcome = %q, want git's own word", decided.Outcome)
	}
	if strings.Contains(decided.Outcome, "a.go") {
		t.Errorf("the detail reached the card rather than the log: %q", decided.Outcome)
	}
}

// Declining performs nothing and says who declined it where.
func TestDeclinePerformsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbDeleteSession, proposalArgs(VerbDeleteSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	decided, err := svc.Decide(ctx, SurfaceThread, payload["proposal_id"].(string), false)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalDeclined {
		t.Fatalf("status = %q, want declined", decided.Status)
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("a decline performed %v", performed)
	}
}

// A row that is not open answers itself, unchanged. Two surfaces can hold the
// same card, and the second one to press must learn what happened rather than
// be told it broke something.
func TestDecidingATwiceDecidedProposalAnswersItself(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbArchiveSession, proposalArgs(VerbArchiveSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id := payload["proposal_id"].(string)

	if _, err := svc.Decide(ctx, SurfaceThread, id, false); err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	again, err := svc.Decide(ctx, SurfaceVoice, id, true)
	if err != nil {
		t.Fatalf("Decide() on a decided row = %v, want the row", err)
	}
	if again.Status != ProposalDeclined {
		t.Errorf("status = %q, want the decision that was already made", again.Status)
	}
	if again.DecidedVia != SurfaceThread {
		t.Errorf("decidedVia = %q, want the surface that actually decided it", again.DecidedVia)
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("a second yes on a declined proposal performed %v", performed)
	}
}

// An undecided proposal stops being an offer. Expiry is derived on the read and
// written on the next decide, so a pure read stays pure.
func TestAProposalExpires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)}
	svc, actions, _ := proposalWorld(t, WithClock(clock.now))

	payload, err := svc.Invoke(ctx, VerbReclaimSession, proposalArgs(VerbReclaimSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id := payload["proposal_id"].(string)

	clock.advance(proposalTTL + time.Hour)

	open, err := svc.OpenProposals(ctx)
	if err != nil {
		t.Fatalf("OpenProposals() = %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("%d proposals are still offers past the TTL", len(open))
	}

	decided, err := svc.Decide(ctx, SurfaceThread, id, true)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalExpired {
		t.Fatalf("status = %q, want expired", decided.Status)
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("an expired yes performed %v", performed)
	}
}

// The head can ask and it cannot decide. There is no verb for it, and no verb
// it does have moves a proposal off open.
func TestTheHeadHasNoVerbThatDecides(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbMergeSession, proposalArgs(VerbMergeSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id := payload["proposal_id"].(string)

	for _, verb := range svc.Verbs() {
		for _, forbidden := range []string{"decide", "accept", "approve", "confirm_proposal"} {
			if strings.Contains(verb.Name, forbidden) {
				t.Errorf("%q is in the table: the yes is not the head's to give", verb.Name)
			}
		}
		// Whatever it is handed, no verb may settle a proposal.
		if _, err := svc.Invoke(ctx, verb.Name, map[string]any{
			"id": id, "proposal_id": id, "accept": true,
		}); err != nil {
			continue
		}
	}

	after, err := svc.Proposal(ctx, id)
	if err != nil {
		t.Fatalf("Proposal() = %v", err)
	}
	if after.Status != ProposalOpen {
		t.Fatalf("status = %q after the head called every verb it has", after.Status)
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Fatalf("the head performed %v through the verb table", performed)
	}
}

// A proposal is announced twice over: pushed for a page that is already open,
// and handed to every surface so a blind one can say it out loud.
func TestAProposalIsPushedAndDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "the retry fix", ProjectName: "riff"}}}
	actions := newFakeActions()
	svc, _, recorder := newTestService(t, WithDirectory(dir), WithActions(actions))
	thread := &fakeSurface{name: SurfaceThread, cards: true}
	release, err := svc.RegisterSurface(thread)
	if err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}
	defer release()

	payload, err := svc.Invoke(ctx, VerbArchiveSession, proposalArgs(VerbArchiveSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if _, err := svc.Decide(ctx, SurfaceThread, payload["proposal_id"].(string), false); err != nil {
		t.Fatalf("Decide() = %v", err)
	}

	if got := thread.countOf(ItemProposal); got != 2 {
		t.Errorf("the surface was handed %d proposals, want one on create and one on decide", got)
	}
	for _, item := range thread.got() {
		if item.Kind == ItemProposal && item.Proposal == nil {
			t.Error("an ItemProposal carries no proposal")
		}
	}
	if n := countEvents(recorder, EventProposal); n != 2 {
		t.Errorf("%d %s pushes, want one on create and one on decide", n, EventProposal)
	}
}

// A mode the setter would silently coerce is refused instead. The setter
// underneath turns an unknown mode into "default", so an unvalidated proposal
// would be accepted, change the session to something nobody asked for, and
// report success.
func TestAnUnknownPermissionModeIsRefused(t *testing.T) {
	t.Parallel()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(context.Background(), VerbSetSessionMode, map[string]any{
		"session_id": "s1", "rationale": "it keeps stopping", "mode": "yolo",
	})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	refusal, refused := payload["error"].(string)
	if !refused {
		t.Fatalf("payload = %v, want a refusal", payload)
	}
	if !strings.Contains(refusal, PermissionModeAcceptEdits) {
		t.Errorf("the refusal does not name the closed set: %q", refusal)
	}
	if actions.reads("settings") != 0 {
		t.Error("an invalid mode still read the session's settings")
	}
}

// A session on another machine cannot be proposed about, for the same reason it
// cannot be dispatched to: the world snapshot it came from is a view.
func TestAProposalRefusesARemoteSession(t *testing.T) {
	t.Parallel()
	dir := &fakeDirectory{sessions: []SessionRow{{
		ID: "s9", Name: "the other one", ProjectName: "riff",
		MachineID: "elsewhere", MachineName: "the laptop",
	}}}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithActions(newFakeActions()))

	payload, err := svc.Invoke(context.Background(), VerbMergeSession, map[string]any{
		"session_id": "s9", "rationale": "it is finished",
	})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	refusal, refused := payload["error"].(string)
	if !refused {
		t.Fatalf("payload = %v, want a refusal", payload)
	}
	if !strings.Contains(refusal, "the laptop") {
		t.Errorf("the refusal does not name the machine: %q", refusal)
	}
}

// A card that could only ever fail is worse than a refusal, and the two set_*
// verbs are where that bites hardest: codex's adapter implements neither model
// switching nor a permission mode, and the app's own controls are gated on the
// same capability bits. So a codex target is refused in words that name it, and
// no row is written.
func TestTheSetVerbsRefuseAProviderThatCannotDoIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, verb := range []string{VerbSetSessionModel, VerbSetSessionMode} {
		svc, actions, _ := proposalWorld(t)
		actions.settings = codexSettings()

		payload, err := svc.Invoke(ctx, verb, proposalArgs(verb))
		if err != nil {
			t.Fatalf("Invoke(%q) = %v", verb, err)
		}
		refusal, refused := payload["error"].(string)
		if !refused {
			t.Fatalf("Invoke(%q) = %v, want a refusal rather than a card", verb, payload)
		}
		if !strings.Contains(refusal, "codex") {
			t.Errorf("%s refusal does not name the CLI that cannot do it: %q", verb, refusal)
		}
		open, err := svc.OpenProposals(ctx)
		if err != nil {
			t.Fatalf("OpenProposals() = %v", err)
		}
		if len(open) != 0 {
			t.Errorf("%s wrote %d cards for something that can only fail", verb, len(open))
		}
	}
}

// A spoken family name is resolved against the TARGET's catalog, not claude's:
// the target of set_session_model is an existing session whose provider is
// knowable, and claude's answer is a slug that session cannot run.
func TestAModelIsResolvedAgainstTheTargetsProvider(t *testing.T) {
	t.Parallel()
	svc, actions, _ := proposalWorld(t)
	actions.settings.Provider = "some-other-cli"

	if _, err := svc.Invoke(context.Background(), VerbSetSessionModel,
		proposalArgs(VerbSetSessionModel)); err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if got := actions.resolvedAgainst(); got != "some-other-cli" {
		t.Errorf("resolved against %q, want the target's own provider", got)
	}
}

// A decision outlives its caller. The socket that carried the yes can close
// mid-merge and the voice path's context is a 30-second tool budget, so an
// executor that runs while the request context dies must still leave a settled
// row — one left `open` after the action is one the next press performs again.
func TestADecisionIsRecordedWhenItsCallerGoesAway(t *testing.T) {
	t.Parallel()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(context.Background(), VerbArchiveSession,
		proposalArgs(VerbArchiveSession))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id, _ := payload["proposal_id"].(string)

	// The caller goes away while the executor works, which is what a closed tab
	// mid-merge looks like from here.
	dying, cancel := context.WithCancel(context.Background())
	actions.onExec = cancel

	decided, err := svc.Decide(dying, SurfaceThread, id, true)
	if err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if decided.Status != ProposalAccepted {
		t.Fatalf("status = %q (outcome %q), want accepted", decided.Status, decided.Outcome)
	}

	stored, err := svc.Proposal(context.Background(), id)
	if err != nil {
		t.Fatalf("Proposal() = %v", err)
	}
	if stored.Status != ProposalAccepted {
		t.Fatalf("the row is %q after the action was performed -- the next press would archive again",
			stored.Status)
	}
	if stored.DecidedAt == "" || stored.DecidedVia != SurfaceThread {
		t.Errorf("the decision was not recorded: %+v", stored)
	}
	// And it cannot be performed twice.
	if _, err := svc.Decide(context.Background(), SurfaceThread, id, true); err != nil {
		t.Fatalf("Decide() = %v", err)
	}
	if performed := actions.performed(); len(performed) != 1 {
		t.Errorf("performed %v, want exactly one archive", performed)
	}
}

// One open card per verb and target, under concurrency. The read-then-insert
// this used to be is not enforcement: two tool calls in one head message run on
// their own goroutines, both see no open row, and both insert. The rule lives in
// a partial unique index now, and the insert that loses answers "already
// waiting".
func TestOnlyOneOpenProposalSurvivesConcurrentAsks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, _ := proposalWorld(t)

	const askers = 8
	var wg sync.WaitGroup
	wg.Add(askers)
	for range askers {
		go func() {
			defer wg.Done()
			if _, err := svc.Invoke(ctx, VerbArchiveSession, proposalArgs(VerbArchiveSession)); err != nil {
				t.Errorf("Invoke() = %v", err)
			}
		}()
	}
	wg.Wait()

	open, err := svc.OpenProposals(ctx)
	if err != nil {
		t.Fatalf("OpenProposals() = %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("%d open cards for one verb and one target, want 1 -- accepting either would leave "+
			"the others pointing at work already done", len(open))
	}
}

// Dissolve's destructive half is never the default. prepare writes keep_history
// in, but the args column is JSON and an unreadable one decodes to nothing --
// and the verb whose argument went missing must not perform the irreversible
// variant of itself.
func TestADissolveWithNoArgsKeepsTheHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	payload, err := svc.Invoke(ctx, VerbDissolveChannel, proposalArgs(VerbDissolveChannel))
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	id, _ := payload["proposal_id"].(string)

	// What a column that did not round-trip looks like by the time exec sees it.
	p, err := svc.Proposal(ctx, id)
	if err != nil {
		t.Fatalf("Proposal() = %v", err)
	}
	p.Args = nil
	if _, err := execDissolve(ctx, svc, p); err != nil {
		t.Fatalf("execDissolve() = %v", err)
	}
	if performed := actions.performed(); len(performed) != 1 || performed[0] != "dissolve-keep" {
		t.Errorf("performed %v, want the half that keeps the record", performed)
	}
}

// The same rule for the two set_* executors: a stored argument that is not one
// of the values prepare validated is an outcome, never a call into the setter.
// SetPermissionMode COERCES an unknown mode to "default", which is exactly the
// "changed something nobody asked for and reported success" this guards.
func TestASetVerbWithAnUnreadableArgumentChangesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, actions, _ := proposalWorld(t)

	for _, exec := range []func(context.Context, *Service, Proposal) (string, error){
		execSetMode, execSetModel,
	} {
		if _, err := exec(ctx, svc, Proposal{SessionID: "s1"}); err == nil {
			t.Error("an empty argument reached the setter")
		}
	}
	if performed := actions.performed(); len(performed) != 0 {
		t.Errorf("performed %v on arguments nobody could read", performed)
	}
}

// testClock is a clock a test can move.
type testClock struct {
	at time.Time
}

func (c *testClock) now() time.Time { return c.at }

func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// countEvents counts the pushes of one type.
func countEvents(recorder *eventbus.Recorder, eventType string) int {
	n := 0
	for _, event := range recorder.Events() {
		if event.Type == eventType {
			n++
		}
	}
	return n
}

// journalKindsIn counts the journal entries of one kind.
func journalKindsIn(t *testing.T, svc *Service, kind JournalKind) int {
	t.Helper()
	entries, err := svc.Journal(context.Background(), "", 200)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	n := 0
	for _, entry := range entries {
		if entry.Kind == kind {
			n++
		}
	}
	return n
}
