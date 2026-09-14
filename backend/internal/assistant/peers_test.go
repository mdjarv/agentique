package assistant

import (
	"context"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

func seedMachine(t *testing.T, queries *store.Queries, id string) {
	t.Helper()
	if err := queries.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: id, Label: id, BaseUrl: "https://" + id, AddedAt: "x",
	}); err != nil {
		t.Fatal(err)
	}
}

// A paired machine's news is kept only for a session this server follows, and
// kept the way a local turn's is: journaled, untrusted, placed on its machine.
func TestIngestPeerNewsForFollowedSessionsOnly(t *testing.T) {
	svc, queries, _ := newTestService(t)
	seedMachine(t, queries, "zbook")
	ctx := context.Background()

	report, _ := ParseReport("surprise", "the loader was already failing")
	svc.IngestPeerReport(ctx, "zbook", "remote-1", report)
	svc.IngestPeerTurnEnd(ctx, "zbook", "remote-1", "Plugin Testing", Notice{Kind: NoticeFinished, Headline: "done"})
	if entries, _ := svc.Journal(ctx, "", 50); len(entries) != 0 {
		t.Fatalf("unfollowed news was journaled: %+v", entries)
	}

	row := SessionRow{ID: "remote-1", Name: "Plugin Testing", MachineID: "zbook", MachineName: "zbook", Reach: ReachPeer}
	if err := svc.FollowRow(ctx, row, "dispatch"); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if !svc.Following(ctx, "remote-1") {
		t.Fatal("a followed remote session is not Following")
	}

	svc.IngestPeerReport(ctx, "zbook", "remote-1", report)
	svc.IngestPeerTurnEnd(ctx, "zbook", "remote-1", "Plugin Testing", Notice{Kind: NoticeFailed, Headline: "tests fail"})
	entries, err := svc.Journal(ctx, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[JournalKind]JournalEntry{}
	for _, e := range entries {
		kinds[e.Kind] = e
	}
	rep, ok := kinds[JournalReport]
	if !ok || !rep.Untrusted || rep.Payload["machine"] != "zbook" || rep.ProjectID != "" {
		t.Fatalf("report entry = %+v", rep)
	}
	failed, ok := kinds[JournalSessionFailed]
	if !ok || failed.Payload["name"] != "Plugin Testing on zbook" || failed.Payload["machine"] != "zbook" || failed.ProjectID != "" {
		t.Fatalf("turn end entry = %+v", failed)
	}

	if err := svc.Unfollow(ctx, "remote-1"); err != nil {
		t.Fatal(err)
	}
	if svc.Following(ctx, "remote-1") {
		t.Fatal("unfollow left the remote follow")
	}
}

// statefulDirectory answers remote sessions' state for the budget.
type statefulDirectory struct {
	*fakeDirectory
	states map[string][2]bool // sessionID -> {unfinished, known}
}

func (d *statefulDirectory) Unfinished(_ context.Context, _, sessionID string) (bool, bool) {
	st, ok := d.states[sessionID]
	if !ok {
		return false, false
	}
	return st[0], st[1]
}

// A policy's sessions on paired machines count against its in-flight budget,
// and one whose machine does not answer counts as still open.
func TestInFlightBudgetCountsPairedMachines(t *testing.T) {
	ctx := context.Background()
	base := &fakeDirectory{
		projects: []ProjectRow{{ID: "zp", Name: "seisiun", MachineID: "zbook", MachineName: "zbook", Reach: ReachPeer, AcceptsPolicies: true}},
		created:  SessionRow{ID: "remote-a", Name: "nightly", MachineID: "zbook", MachineName: "zbook", Reach: ReachPeer, AcceptsPolicies: true},
	}
	dir := &statefulDirectory{fakeDirectory: base, states: map[string][2]bool{}}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(&fakeDispatcher{}))
	seedMachine(t, queries, "zbook")
	policy := enablePolicy(t, svc, "nightly tests", 1, 10)

	first, err := svc.Invoke(ctx, VerbCreateSession, map[string]any{"project": "seisiun", "machine": "zbook", "policy": policy.Name})
	if err != nil || first["error"] != nil {
		t.Fatalf("first create = %v, %v", first, err)
	}

	// Still running on zbook: the budget of one is spent.
	dir.states["remote-a"] = [2]bool{true, true}
	second, _ := svc.Invoke(ctx, VerbCreateSession, map[string]any{"project": "seisiun", "machine": "zbook", "policy": policy.Name})
	if reason, _ := second[reasonKey].(string); reason != "budget-in-flight" {
		t.Fatalf("with the remote session open: %v", second)
	}

	// zbook asleep: unknown counts as open.
	delete(dir.states, "remote-a")
	third, _ := svc.Invoke(ctx, VerbCreateSession, map[string]any{"project": "seisiun", "machine": "zbook", "policy": policy.Name})
	if reason, _ := third[reasonKey].(string); reason != "budget-in-flight" {
		t.Fatalf("with zbook not answering: %v", third)
	}

	// Finished on zbook: the slot comes back.
	dir.states["remote-a"] = [2]bool{false, true}
	base.created = SessionRow{ID: "remote-b", Name: "nightly 2", MachineID: "zbook", MachineName: "zbook", Reach: ReachPeer, AcceptsPolicies: true}
	fourth, _ := svc.Invoke(ctx, VerbCreateSession, map[string]any{"project": "seisiun", "machine": "zbook", "policy": policy.Name})
	if fourth["error"] != nil {
		t.Fatalf("with the remote session finished: %v", fourth)
	}
}

func TestFindingsAreJournaledInWords(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	svc.IngestFinding(ctx, Finding{Kind: "cli-signed-out", Subject: "claude", Severity: "warning", Remedy: "hand",
		Facts: map[string]any{"agent": "Claude", "help": "Run `claude auth login` to restore usage."}, Opened: true, Machine: "zbook"})
	svc.IngestFinding(ctx, Finding{Kind: "disk-low", Severity: "warning", Remedy: "reclaim",
		Facts: map[string]any{"freeBytes": float64(1 << 30), "reclaimableBytes": float64(11 << 30)}, Opened: true})
	svc.IngestFinding(ctx, Finding{Kind: "a-future-kind", Opened: false, Machine: "zbook"})

	entries, _ := svc.Journal(ctx, "", 10)
	var got []string
	for _, e := range entries {
		if e.Kind != JournalFinding || e.Untrusted {
			t.Fatalf("entry = %+v, want a trusted finding", e)
		}
		got = append(got, e.Summary)
	}
	want := []string{
		"zbook: a-future-kind resolved",
		"this machine: only 1.0 GB free on the data disk; 11.0 GB could be reclaimed from finished sessions",
		"zbook: Claude is signed out, so nothing can run on it there. Run `claude auth login` to restore usage.",
	}
	if len(got) != len(want) {
		t.Fatalf("summaries = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("summary %d = %q, want %q", i, got[i], want[i])
		}
	}
}
