package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Policies are standing instructions, and their budgets are the containment.
//
// A policy is the operator's own sentence about what the assistant may do
// without being asked — "when a session finishes and the tests pass, say so;
// when one is stuck on an approval, do not queue anything behind it". The
// heartbeat reads the enabled ones, a Haiku one-shot judges the journal against
// them, and only `act` wakes the head.
//
// Nothing here widens a tier. What a policy can reach is what a conversation
// could ask for: the read verbs and the contained ones, with anything
// uncontained still a proposal. What a policy adds is a BUDGET, because the
// operator is not in the room — a standing instruction with no ceiling is an
// afternoon of allowance spent on a repository nobody looked at.
//
// The budgets are counted from the JOURNAL rather than from the policy row: an
// edit must not be able to retroactively widen what has already been spent, and
// the journal is the record of what was done and under which instruction.

const (
	// maxPolicyText bounds one policy. A policy is a paragraph — the head reads
	// every enabled one on every triage, so a document here is a document in
	// every prompt.
	maxPolicyText = 8 << 10
	// maxPolicyName bounds the name. It is spoken back in refusals and supplied
	// by the head as an argument, so it is a label, not a sentence.
	maxPolicyName = 120
	// defaultBudgetInFlight and defaultBudgetPerDay are what an unset budget
	// means. They are deliberately small: a policy that wants more says so.
	defaultBudgetInFlight = 1
	defaultBudgetPerDay   = 3
	// policyInFlightWindow is how far back the in-flight count looks for the
	// sessions a policy created.
	//
	// The day cap is measured from midnight, but a session started last night
	// and still running is still in flight this morning — counting only today's
	// would let a policy hold twice its in-flight budget across midnight.
	policyInFlightWindow = 14 * 24 * time.Hour
)

// EventPolicy carries a [Policy] on save and on delete, on the global topic
// like every other assistant push.
const EventPolicy = "assistant.policy"

// Policy is one standing instruction on the wire.
//
// Every field is optional, as every wire field here is: the generated Zod
// schema mirrors these tags, and a required field makes a client reject the
// whole payload from a peer that does not send it.
type Policy struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Text is the instruction itself, in the operator's words.
	Text string `json:"text,omitempty"`
	// Enabled says the heartbeat reads it. A disabled policy is kept: turning
	// one off is not throwing it away.
	Enabled bool `json:"enabled,omitempty"`
	// BudgetInFlight is how many sessions it may have running at once, and
	// BudgetPerDay how many it may create in a day. Absent or zero on the wire
	// means the default, never "none allowed": a client that omits a field must
	// not silently produce an inert policy.
	BudgetInFlight int `json:"budgetInFlight,omitempty"`
	BudgetPerDay   int `json:"budgetPerDay,omitempty"`
	// LastFiredAt is when the heartbeat last acted under it, "" for never.
	LastFiredAt string `json:"lastFiredAt,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	// Deleted is a PUSH-ONLY marker and never a column: `assistant.policy`
	// announces a save and a delete on the same event, so the row that is gone
	// arrives carrying this rather than as a second event type a client would
	// have to know about. A read never sets it.
	Deleted bool `json:"deleted,omitempty"`
}

// Policies returns every policy, enabled or not, in name order.
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	rows, err := s.store.ListAssistantPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	out := make([]Policy, 0, len(rows))
	for _, row := range rows {
		out = append(out, policyFrom(row))
	}
	return out, nil
}

// SavePolicy writes a policy and pushes it. An empty id is a new one.
//
// The validation is here rather than only at the socket because the core is
// what every caller reaches: a name is required (it is what a budget refusal
// names and what the head supplies as an argument), and the text is capped
// rather than truncated — silently storing half an instruction is worse than
// refusing the save.
func (s *Service) SavePolicy(ctx context.Context, p Policy) (Policy, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return Policy{}, errors.New("assistant: a policy needs a name")
	}
	if len(name) > maxPolicyName {
		return Policy{}, fmt.Errorf("assistant: a policy name is at most %d characters", maxPolicyName)
	}
	text := strings.TrimSpace(p.Text)
	if len(text) > maxPolicyText {
		return Policy{}, fmt.Errorf("assistant: a policy is at most %d characters and this one is %d",
			maxPolicyText, len(text))
	}

	id := strings.TrimSpace(p.ID)
	if id == "" {
		id = uuid.New().String()
	}
	now := formatTime(s.now())
	row, err := s.store.UpsertAssistantPolicy(ctx, store.UpsertAssistantPolicyParams{
		ID:             id,
		Name:           name,
		Text:           text,
		Enabled:        boolToInt(p.Enabled),
		BudgetInFlight: int64(budgetOr(p.BudgetInFlight, defaultBudgetInFlight)),
		BudgetPerDay:   int64(budgetOr(p.BudgetPerDay, defaultBudgetPerDay)),
		Now:            now,
	})
	if err != nil {
		return Policy{}, fmt.Errorf("save policy %q: %w", name, err)
	}

	saved := policyFrom(row)
	s.broadcast(EventPolicy, saved)
	return saved, nil
}

// DeletePolicy removes one and pushes the removal. Deleting one that is not
// there is not an error: the state the caller wanted is the state it gets.
func (s *Service) DeletePolicy(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("assistant: no policy to delete")
	}
	if err := s.store.DeleteAssistantPolicy(ctx, id); err != nil {
		return fmt.Errorf("delete policy %s: %w", id, err)
	}
	s.broadcast(EventPolicy, Policy{ID: id, Deleted: true})
	return nil
}

// enabledPolicies is what the heartbeat triages against. An unreadable table is
// no policies, which is the fail-closed answer: nothing runs.
func (s *Service) enabledPolicies(ctx context.Context) []Policy {
	all, err := s.Policies(ctx)
	if err != nil {
		s.log.Warn("assistant: policies unreadable", "error", err)
		return nil
	}
	out := make([]Policy, 0, len(all))
	for _, p := range all {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

// touchPolicy records that the assistant has just acted under this policy.
//
// Bookkeeping the operator reads, never a budget: what a budget counts is
// journal entries, which an edit cannot rewrite. A failure is logged — the
// action already happened, and refusing to report it afterwards would be the
// wrong half to lose.
func (s *Service) touchPolicy(ctx context.Context, id string) {
	if id == "" {
		return
	}
	at := formatTime(s.now())
	if err := s.store.TouchAssistantPolicy(ctx, store.TouchAssistantPolicyParams{
		LastFiredAt: at,
		Now:         at,
		ID:          id,
	}); err != nil {
		s.log.Warn("assistant: policy not stamped as fired", "policy", id, "error", err)
	}
}

// policySpend is what a policy has already spent today.
type policySpend struct {
	// Today is how many sessions this policy created since local midnight.
	Today int
	// InFlight is how many of the ones it created are still unfinished: not
	// archived and not done or failed. A PARKED one counts — a session whose CLI
	// idle eviction or a restart's reap took away is work that is still there,
	// and it would otherwise hand the slot back on every restart.
	InFlight int
	// AssistantToday is how many sessions of ANY policy the assistant created
	// today, read from the sessions table rather than the journal.
	AssistantToday int
}

// checkPolicyBudget resolves the policy the head named and answers a refusal
// when either budget is spent.
//
// A nil refusal means the verb may proceed, and the returned policy is what it
// runs under. An unknown or disabled name is refused rather than ignored: a
// name the head invented must not buy an unbudgeted action, which is exactly
// what falling back to "no policy" would give it.
//
// The two counts come from different places on purpose. The PER-POLICY count is
// the journal's, because only the journal records which instruction an action
// was taken under, and an edit to the policy row cannot rewrite it. The ceiling
// on the assistant's total for the day is the sessions table's, because a
// journal write is logged rather than fatal and a lost entry must not be able
// to widen a budget. It can only narrow: it is the sum of every enabled
// policy's day budget, so it never refuses what the per-policy caps would have
// allowed between them.
func (s *Service) checkPolicyBudget(ctx context.Context, named string) (Policy, map[string]any) {
	name := strings.TrimSpace(named)
	if name == "" {
		return Policy{}, nil
	}

	all, err := s.Policies(ctx)
	if err != nil {
		return Policy{}, refuse("policies-unreadable", "I could not read my own standing "+
			"instructions, so I did not act on one. Say that plainly.")
	}

	var policy Policy
	for _, p := range all {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			policy = p
			break
		}
	}
	if policy.ID == "" {
		return Policy{}, refuse("policy-unknown", fmt.Sprintf("There is no standing instruction "+
			"called %q, so nothing was done. Only name a policy you were told about in this turn.", name))
	}
	if !policy.Enabled {
		return Policy{}, refuse("policy-disabled", fmt.Sprintf("The %q instruction is turned off, "+
			"so nothing was done under it.", policy.Name))
	}

	spend, err := s.policySpend(ctx, policy)
	if err != nil {
		s.log.Warn("assistant: policy budget unreadable", "policy", policy.ID, "error", err)
		return Policy{}, refuse("budget-unreadable", fmt.Sprintf("I could not work out what the %q "+
			"instruction has already done today, so I did nothing rather than guess.", policy.Name))
	}

	if spend.Today >= policy.BudgetPerDay {
		return Policy{}, refuse("budget-per-day", fmt.Sprintf("The %q instruction has already "+
			"started %d of its %d sessions for today, so nothing was done. Say that, and that it can "+
			"again tomorrow.", policy.Name, spend.Today, policy.BudgetPerDay))
	}
	if ceiling := s.assistantDayCeiling(all); spend.AssistantToday >= ceiling {
		return Policy{}, refuse("budget-assistant-day", fmt.Sprintf("I have already started %d "+
			"sessions on my own today, which is the whole of what my standing instructions allow "+
			"between them (%d), so nothing was done under %q.",
			spend.AssistantToday, ceiling, policy.Name))
	}
	if spend.InFlight >= policy.BudgetInFlight {
		// "Unfinished" rather than "running": the count includes a session whose
		// CLI was reclaimed, because the work is still there and the next
		// message resumes it.
		return Policy{}, refuse("budget-in-flight", fmt.Sprintf("The %q instruction already has %d "+
			"unfinished session, which is its limit of %d at once, so nothing was done. Say that it "+
			"will wait for that one to finish or be filed away.",
			policy.Name, spend.InFlight, policy.BudgetInFlight))
	}
	return policy, nil
}

// policySpend counts what a policy has spent, from the journal and the sessions
// table.
//
// Three counts, all of them COUNTS: the journal is never paged into Go for this.
// The page it used to read was 2000 rows over fourteen days and failed closed
// when it filled, on the sound argument that an undercount is the one error that
// widens a budget — but a machine journaling a row per turn per session fills
// fourteen days with thousands, so the guard's ordinary state on a busy install
// was "every budgeted verb refuses, forever". A count has no page to fill, and
// the fail-closed branch goes with it. Compaction bounds the table now and
// changes nothing here: it folds only days older than [policyInFlightWindow],
// which is exactly the window this reads.
func (s *Service) policySpend(ctx context.Context, policy Policy) (policySpend, error) {
	now := s.now()
	startOfDay := formatTime(startOfLocalDay(now))
	spend := policySpend{}

	today, err := s.store.CountPolicySessionsCreatedSince(ctx, store.CountPolicySessionsCreatedSinceParams{
		Since:    startOfDay,
		PolicyID: policy.ID,
	})
	if err != nil {
		return policySpend{}, fmt.Errorf("count today for the %q budget: %w", policy.Name, err)
	}
	spend.Today = int(today)

	inFlight, err := s.store.CountLiveSessionsForPolicy(ctx, store.CountLiveSessionsForPolicyParams{
		Origin:   OriginAssistant,
		Since:    formatTime(now.Add(-policyInFlightWindow)),
		PolicyID: policy.ID,
	})
	if err != nil {
		return policySpend{}, fmt.Errorf("count in flight for the %q budget: %w", policy.Name, err)
	}
	remoteInFlight, err := s.peerInFlight(ctx, policy, now)
	if err != nil {
		return policySpend{}, fmt.Errorf("count in flight on paired machines for the %q budget: %w", policy.Name, err)
	}
	spend.InFlight = int(inFlight) + remoteInFlight

	total, err := s.store.CountSessionsByOriginSince(ctx, store.CountSessionsByOriginSinceParams{
		Origin: OriginAssistant,
		Since:  startOfDay,
	})
	if err != nil {
		return policySpend{}, fmt.Errorf("count assistant sessions since %s: %w", startOfDay, err)
	}
	remote, err := s.store.CountPeerSessionsCreatedSince(ctx, startOfDay)
	if err != nil {
		return policySpend{}, fmt.Errorf("count assistant sessions on paired machines since %s: %w", startOfDay, err)
	}
	spend.AssistantToday = int(total) + int(remote)
	return spend, nil
}

// peerInFlight counts a policy's unfinished sessions on paired machines
// (docs/peers.md). The journal names them; each machine's own list says
// whether they are still open.
//
// **It fails closed.** A session whose machine does not answer, or a directory
// that cannot ask, counts as in flight: an undercount is the one error that
// widens a budget, and "the laptop is asleep" must not hand a standing
// instruction its slots back while the work on that laptop is still open.
func (s *Service) peerInFlight(ctx context.Context, policy Policy, now time.Time) (int, error) {
	rows, err := s.store.ListPeerPolicySessionsCreatedSince(ctx, store.ListPeerPolicySessionsCreatedSinceParams{
		Since:    formatTime(now.Add(-policyInFlightWindow)),
		PolicyID: policy.ID,
	})
	if err != nil {
		return 0, err
	}
	states, canAsk := s.dir.(PeerSessionStates)
	n := 0
	for _, row := range rows {
		if !canAsk {
			n++
			continue
		}
		unfinished, known := states.Unfinished(ctx, row.MachineID, row.SessionID)
		if unfinished || !known {
			n++
		}
	}
	return n, nil
}

// assistantDayCeiling is the sum of every enabled policy's day budget: the most
// the assistant may start on its own in one day, whatever the journal says
// about which instruction did it.
func (s *Service) assistantDayCeiling(all []Policy) int {
	total := 0
	for _, p := range all {
		if p.Enabled {
			total += p.BudgetPerDay
		}
	}
	return total
}

// OriginAssistant is what `sessions.origin` says for a session the assistant
// created, and what a turn it dispatches carries as its query origin.
//
// It is spelled here as well as in internal/session because this package does
// not import the session pipeline; the two are asserted equal by a test in the
// server package, which imports both.
const OriginAssistant = "assistant"

// payloadPolicyID and payloadPolicyName are how a journal entry records the
// standing instruction an action was taken under. The id is what a budget
// counts, so a rename cannot move a policy's spending; the name is what a
// person reads.
//
// `payloadPolicyID`'s key is spelled a second time as the JSON path
// `$.policyId` in `CountPolicySessionsCreatedSince` and
// `CountLiveSessionsForPolicy`, which is where the counting happens. Renaming it
// here alone leaves both budgets counting nothing.
const (
	payloadPolicyID   = "policyId"
	payloadPolicyName = "policy"
)

// startOfLocalDay is midnight before t in the machine's own timezone.
//
// Local rather than UTC because a day budget is a person's day: "three sessions
// a day" means their day, and on a UTC+13 machine a UTC midnight would reset
// the count in the middle of an afternoon.
func startOfLocalDay(t time.Time) time.Time {
	local := t.Local()
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

// budgetOr is what an unset budget means: the default, never zero. A client
// that omits an optional field must not produce a policy that can do nothing.
func budgetOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// policyFrom maps a stored row to the wire shape.
func policyFrom(row store.AssistantPolicy) Policy {
	return Policy{
		ID:             row.ID,
		Name:           row.Name,
		Text:           row.Text,
		Enabled:        row.Enabled != 0,
		BudgetInFlight: int(row.BudgetInFlight),
		BudgetPerDay:   int(row.BudgetPerDay),
		LastFiredAt:    row.LastFiredAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
