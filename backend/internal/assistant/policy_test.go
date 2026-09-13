package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// A policy needs a name, and its text is capped rather than truncated: storing
// half an instruction is worse than refusing the save.
func TestSavingAPolicyValidatesItsNameAndCapsItsText(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	for _, name := range []string{"", "   "} {
		if _, err := svc.SavePolicy(ctx, Policy{Name: name, Text: "something"}); err == nil {
			t.Errorf("SavePolicy(name=%q) was accepted", name)
		}
	}
	if _, err := svc.SavePolicy(ctx, Policy{Name: strings.Repeat("x", maxPolicyName+1)}); err == nil {
		t.Error("a name longer than the cap was accepted")
	}

	long := strings.Repeat("a", maxPolicyText+1)
	err := func() error { _, err := svc.SavePolicy(ctx, Policy{Name: "nightly", Text: long}); return err }()
	if err == nil {
		t.Fatal("a policy longer than the cap was accepted")
	}
	// The message is formatted FROM the cap, so it cannot outlive it.
	if !strings.Contains(err.Error(), "8192") {
		t.Errorf("error = %q, want it to name the cap", err)
	}
	if policies, listErr := svc.Policies(ctx); listErr != nil || len(policies) != 0 {
		t.Errorf("policies = %v (err %v), want nothing stored", policies, listErr)
	}
}

// An unset budget is the default, never zero: a client that omits a field must
// not write a policy that can do nothing.
func TestAnUnsetBudgetIsTheDefault(t *testing.T) {
	svc, _, recorder := newTestService(t)
	saved, err := svc.SavePolicy(context.Background(), Policy{Name: "nightly tests", Enabled: true})
	if err != nil {
		t.Fatalf("SavePolicy() = %v", err)
	}
	if saved.BudgetInFlight != defaultBudgetInFlight || saved.BudgetPerDay != defaultBudgetPerDay {
		t.Errorf("budgets = (%d, %d), want the defaults (%d, %d)",
			saved.BudgetInFlight, saved.BudgetPerDay, defaultBudgetInFlight, defaultBudgetPerDay)
	}
	if saved.ID == "" {
		t.Error("a new policy got no id")
	}
	if countEvents(recorder, EventPolicy) != 1 {
		t.Error("saving a policy did not push it")
	}
}

// An edit is the same statement, and a delete pushes the row's absence on the
// same event rather than as a second kind.
func TestEditingAndDeletingAPolicy(t *testing.T) {
	svc, _, recorder := newTestService(t)
	ctx := context.Background()

	saved, err := svc.SavePolicy(ctx, Policy{Name: "nightly tests", Text: "one", Enabled: true})
	if err != nil {
		t.Fatalf("SavePolicy() = %v", err)
	}
	edited, err := svc.SavePolicy(ctx, Policy{ID: saved.ID, Name: "nightly tests", Text: "two"})
	if err != nil {
		t.Fatalf("SavePolicy(edit) = %v", err)
	}
	if edited.ID != saved.ID || edited.Text != "two" || edited.Enabled {
		t.Errorf("edited = %+v, want the same row with the new text and disabled", edited)
	}
	if policies, _ := svc.Policies(ctx); len(policies) != 1 {
		t.Errorf("policies = %d, want the edit to have replaced the row", len(policies))
	}

	if err := svc.DeletePolicy(ctx, saved.ID); err != nil {
		t.Fatalf("DeletePolicy() = %v", err)
	}
	if policies, _ := svc.Policies(ctx); len(policies) != 0 {
		t.Error("the policy survived its delete")
	}
	// Deleting one that is gone is the state the caller asked for.
	if err := svc.DeletePolicy(ctx, saved.ID); err != nil {
		t.Errorf("a second DeletePolicy() = %v", err)
	}
	if countEvents(recorder, EventPolicy) != 4 {
		t.Errorf("policy pushes = %d, want one per write", countEvents(recorder, EventPolicy))
	}
}

// A policy that has created its day's sessions refuses the next one, and the
// refusal names the budget and the count.
func TestTheDayBudgetRefusesAndNamesTheCount(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{projects: []ProjectRow{{ID: "p1", Name: "riff"}}}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))

	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	dir.projects = []ProjectRow{{ID: project.ID, Name: "riff"}}
	seeded := testutil.SeedSession(t, queries, project.ID, "idle")
	dir.created = SessionRow{ID: seeded.ID, Name: "the nightly run", ProjectID: project.ID}

	policy := enablePolicy(t, svc, "nightly tests", 5, 1)

	first, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": policy.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	if created, _ := first["created"].(bool); !created {
		t.Fatalf("the first create under a policy was refused: %v", first)
	}

	second, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": policy.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	refusal, _ := second["error"].(string)
	if refusal == "" {
		t.Fatalf("the second create was allowed past the day budget: %v", second)
	}
	for _, want := range []string{"nightly tests", "1"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal = %q, want it to name %q", refusal, want)
		}
	}
	if reason, _ := second[reasonKey].(string); reason != "budget-per-day" {
		t.Errorf("reason = %q, want the day budget", reason)
	}
}

// The in-flight budget counts the policy's own sessions that are still live.
func TestTheInFlightBudgetRefusesWhileItsSessionIsStillRunning(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))

	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	dir.projects = []ProjectRow{{ID: project.ID, Name: "riff"}}
	live := testutil.SeedSession(t, queries, project.ID, "idle")
	// In the server the directory asks for this origin at creation and
	// CreateSession stamps it through the same query; the fake directory does
	// neither, so the test does what the wiring does.
	if err := queries.SetSessionOrigin(ctx, store.SetSessionOriginParams{
		Origin: OriginAssistant, ID: live.ID,
	}); err != nil {
		t.Fatalf("SetSessionOrigin() = %v", err)
	}
	dir.created = SessionRow{ID: live.ID, Name: "the nightly run", ProjectID: project.ID}

	policy := enablePolicy(t, svc, "nightly tests", 1, 5)

	if _, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": policy.Name,
	}); err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}

	second, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": policy.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	if reason, _ := second[reasonKey].(string); reason != "budget-in-flight" {
		t.Fatalf("reason = %q, want the in-flight budget (payload %v)", reason, second)
	}
	refusal, _ := second["error"].(string)
	if !strings.Contains(refusal, "nightly tests") || !strings.Contains(refusal, "1") {
		t.Errorf("refusal = %q, want it to name the policy and the count", refusal)
	}
}

// The ceiling is the backstop: it counts the sessions TABLE, so a journal entry
// that never landed cannot widen what the assistant may start in a day.
func TestTheAssistantsDayCeilingCountsTheSessionsTable(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))

	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	dir.projects = []ProjectRow{{ID: project.ID, Name: "riff"}}
	// Two assistant-origin sessions today with NO journal entry naming a policy:
	// what a lost journal write looks like from here.
	for range 2 {
		row := testutil.SeedSession(t, queries, project.ID, "done")
		if err := queries.SetSessionOrigin(ctx, store.SetSessionOriginParams{
			Origin: OriginAssistant, ID: row.ID,
		}); err != nil {
			t.Fatalf("SetSessionOrigin() = %v", err)
		}
	}
	target := testutil.SeedSession(t, queries, project.ID, "idle")
	dir.created = SessionRow{ID: target.ID, Name: "the nightly run", ProjectID: project.ID}

	policy := enablePolicy(t, svc, "nightly tests", 5, 2)

	payload, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": policy.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "budget-assistant-day" {
		t.Fatalf("reason = %q, want the assistant's own ceiling (payload %v)", reason, payload)
	}
}

// Without a policy the verbs are unbudgeted: the operator asked, in a
// conversation they can read.
func TestWithNoPolicyThereIsNoBudget(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))

	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	dir.projects = []ProjectRow{{ID: project.ID, Name: "riff"}}
	seeded := testutil.SeedSession(t, queries, project.ID, "idle")
	if err := queries.SetSessionOrigin(ctx, store.SetSessionOriginParams{
		Origin: OriginAssistant, ID: seeded.ID,
	}); err != nil {
		t.Fatalf("SetSessionOrigin() = %v", err)
	}
	dir.created = SessionRow{ID: seeded.ID, Name: "the nightly run", ProjectID: project.ID}
	enablePolicy(t, svc, "nightly tests", 1, 1)

	// Three in a row, past every budget the one policy has.
	for i := range 3 {
		payload, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{"project": "riff"})
		if err != nil {
			t.Fatalf("Invoke(create) = %v", err)
		}
		if created, _ := payload["created"].(bool); !created {
			t.Fatalf("create %d without a policy was refused: %v", i, payload)
		}
	}
}

// A name that is not a policy is refused rather than treated as no policy: the
// fallback would be an unbudgeted action bought with an invented word.
func TestAnInventedPolicyNameIsRefused(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))
	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	dir.projects = []ProjectRow{{ID: project.ID, Name: "riff"}}
	dir.created = SessionRow{ID: testutil.SeedSession(t, queries, project.ID, "idle").ID}

	payload, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": "whatever I like",
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "policy-unknown" {
		t.Errorf("reason = %q, want the unknown policy (payload %v)", reason, payload)
	}

	// A real policy that is turned off is refused too, and says which.
	off, err := svc.SavePolicy(ctx, Policy{Name: "nightly tests", Text: "x"})
	if err != nil {
		t.Fatalf("SavePolicy() = %v", err)
	}
	payload, err = svc.Invoke(ctx, VerbCreateSession, map[string]any{
		"project": "riff", "policy": off.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(create) = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "policy-disabled" {
		t.Errorf("reason = %q, want the disabled policy (payload %v)", reason, payload)
	}
}

// A dispatch under a policy carries it to the turn, and the journal entry names
// it — which is what the budgets count and what a person reads afterwards.
func TestADispatchUnderAPolicyCarriesItAndIsJournaled(t *testing.T) {
	ctx := context.Background()
	dispatcher := &fakeDispatcher{}
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "the retry fix", ProjectName: "riff"}}}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(dispatcher))

	project := testutil.SeedProject(t, queries, "riff", t.TempDir())
	seeded := testutil.SeedSession(t, queries, project.ID, "idle")
	dir.sessions[0].ID = seeded.ID
	policy := enablePolicy(t, svc, "nightly tests", 3, 3)

	payload, err := svc.Invoke(ctx, VerbRunPrompt, map[string]any{
		"session_id": seeded.ID, "prompt": "run the tests again", "policy": policy.Name,
	})
	if err != nil {
		t.Fatalf("Invoke(run_prompt) = %v", err)
	}
	if refusal, _ := payload["error"].(string); refusal != "" {
		t.Fatalf("the dispatch was refused: %s", refusal)
	}
	if under := dispatcher.underPolicies(); len(under) != 1 || under[0] != policy.ID {
		t.Errorf("the turn carried %v, want the policy id %q", under, policy.ID)
	}

	entries, err := svc.Journal(ctx, "", 20)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	var found bool
	for _, entry := range entries {
		if entry.Kind != JournalDispatched {
			continue
		}
		found = true
		if entry.Payload[payloadPolicyID] != policy.ID || entry.Payload[payloadPolicyName] != policy.Name {
			t.Errorf("dispatch entry payload = %v, want the policy on it", entry.Payload)
		}
	}
	if !found {
		t.Error("the dispatch was not journaled")
	}

	// And the policy is stamped as having fired, which is what the page shows.
	policies, err := svc.Policies(ctx)
	if err != nil {
		t.Fatalf("Policies() = %v", err)
	}
	if policies[0].LastFiredAt == "" {
		t.Error("the policy was not stamped as fired")
	}
}

// A dispatch the operator asked for carries no policy at all.
func TestADispatchWithoutAPolicyCarriesNone(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "the retry fix"}}}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithDispatcher(dispatcher))

	if _, err := svc.Invoke(context.Background(), VerbRunPrompt, map[string]any{
		"session_id": "s1", "prompt": "run the tests",
	}); err != nil {
		t.Fatalf("Invoke(run_prompt) = %v", err)
	}
	if under := dispatcher.underPolicies(); len(under) != 1 || under[0] != "" {
		t.Errorf("the turn carried %v, want no policy", under)
	}
}
