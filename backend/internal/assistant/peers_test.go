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
	if !ok || failed.Payload["name"] != "Plugin Testing on zbook" || failed.ProjectID != "" {
		t.Fatalf("turn end entry = %+v", failed)
	}

	if err := svc.Unfollow(ctx, "remote-1"); err != nil {
		t.Fatal(err)
	}
	if svc.Following(ctx, "remote-1") {
		t.Fatal("unfollow left the remote follow")
	}
}
