package peer

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

type outboxClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *outboxClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *outboxClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fakeFacts struct {
	pending string
	outcome assistant.TurnOutcome
}

func (f fakeFacts) PendingHumanInput(string) string { return f.pending }

func (f fakeFacts) TurnOutcome(context.Context, string) (assistant.TurnOutcome, error) {
	return f.outcome, nil
}

// newOutboxDB is a migrated database with one project and two sessions, so the
// follows table's foreign key is exercised rather than assumed.
func newOutboxDB(t *testing.T) *store.Queries {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO projects (id, name, path, slug) VALUES ('p1', 'seisiun', '/tmp/p1', 'seisiun')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for _, id := range []string{"s1", "s2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO sessions (id, project_id, name, work_dir) VALUES (?, 'p1', ?, '/tmp/p1')`, id, id); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}
	return store.New(db)
}

func newTestOutbox(t *testing.T, facts assistant.TurnFacts) (*Outbox, *outboxClock) {
	t.Helper()
	clock := &outboxClock{t: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	return NewOutbox(newOutboxDB(t), facts, withOutboxClock(clock.now)), clock
}

func mustReport(t *testing.T, headline string) assistant.Report {
	t.Helper()
	r, err := assistant.ParseReport("surprise", headline)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOutboxKeepsReportsOnlyForFollowedSessions(t *testing.T) {
	o, _ := newTestOutbox(t, nil)
	ctx := context.Background()

	if kept, err := o.Report(ctx, "s1", mustReport(t, "nobody asked")); err != nil || kept {
		t.Fatalf("unfollowed report kept=%v err=%v", kept, err)
	}
	if err := o.Follow(ctx, "s1", "cred-review", ""); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if kept, err := o.Report(ctx, "s1", mustReport(t, "the loader was already failing")); err != nil || !kept {
		t.Fatalf("followed report kept=%v err=%v", kept, err)
	}

	got, err := o.Events(ctx, "cred-review", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].Kind != EventReport || got.Latest != got.Events[0].Seq {
		t.Fatalf("events = %+v", got)
	}
	var payload map[string]any
	_ = json.Unmarshal(got.Events[0].Payload, &payload)
	if payload["headline"] != "the loader was already failing" || payload["untrusted"] != true {
		t.Fatalf("payload = %v", payload)
	}

	// Another credential sees none of it.
	other, _ := o.Events(ctx, "cred-other", 0, 0)
	if len(other.Events) != 0 || other.Latest != 0 {
		t.Fatalf("another credential read %+v", other)
	}
	// And a cursor past the row returns nothing again: the log is not a queue.
	again, _ := o.Events(ctx, "cred-review", got.Latest, 0)
	if len(again.Events) != 0 {
		t.Fatalf("read past the cursor = %+v", again)
	}
}

func TestOutboxReportBudget(t *testing.T) {
	o, clock := newTestOutbox(t, nil)
	ctx := context.Background()
	_ = o.Follow(ctx, "s1", "cred", "")
	for i := 0; i < reportsPerWindow; i++ {
		if kept, _ := o.Report(ctx, "s1", mustReport(t, "x")); !kept {
			t.Fatalf("report %d dropped under the budget", i)
		}
	}
	if kept, _ := o.Report(ctx, "s1", mustReport(t, "x")); kept {
		t.Fatal("report over the budget was kept")
	}
	clock.advance(reportWindow + time.Second)
	if kept, _ := o.Report(ctx, "s1", mustReport(t, "x")); !kept {
		t.Fatal("budget did not refill")
	}
}

func TestOutboxRecordsTurnEndsWithTheirOutcome(t *testing.T) {
	facts := fakeFacts{outcome: assistant.TurnOutcome{Failed: true, SessionName: "Plugin Testing", ClosingWords: "tests fail on main"}}
	o, _ := newTestOutbox(t, facts)
	ctx := context.Background()
	_ = o.Follow(ctx, "s1", "cred", "nightly")

	o.recordTurnEnd("s2") // not followed
	o.recordTurnEnd("s1")

	got, _ := o.Events(ctx, "cred", 0, 0)
	if len(got.Events) != 1 || got.Events[0].SessionID != "s1" || got.Events[0].Kind != EventTurnEnd {
		t.Fatalf("events = %+v", got)
	}
	var payload map[string]any
	_ = json.Unmarshal(got.Events[0].Payload, &payload)
	if payload["kind"] != string(assistant.NoticeFailed) || payload["name"] != "Plugin Testing" ||
		payload["headline"] != "tests fail on main" || payload["untrusted"] != true {
		t.Fatalf("payload = %v", payload)
	}
}

// A poll with nothing to read waits, and a row arriving wakes it at once rather
// than at the end of the wait.
func TestOutboxPollWakesOnNews(t *testing.T) {
	o, _ := newTestOutbox(t, nil)
	ctx := context.Background()
	_ = o.Follow(ctx, "s1", "cred", "")

	done := make(chan EventsResponse, 1)
	start := time.Now()
	go func() {
		out, _ := o.Events(ctx, "cred", 0, 10*time.Second)
		done <- out
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := o.Report(ctx, "s1", mustReport(t, "news")); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if len(out.Events) != 1 {
			t.Fatalf("woken poll = %+v", out)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("poll slept through the news")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poll never woke")
	}
}

func TestOutboxPollWaitIsBounded(t *testing.T) {
	o, _ := newTestOutbox(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := o.Events(ctx, "cred", 0, time.Hour)
	if err != nil || len(out.Events) != 0 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a cancelled poll kept waiting")
	}
}

func TestOutboxAgesOutUndeliveredNews(t *testing.T) {
	o, clock := newTestOutbox(t, nil)
	ctx := context.Background()
	_ = o.Follow(ctx, "s1", "cred", "")
	_, _ = o.Report(ctx, "s1", mustReport(t, "old"))

	clock.advance(outboxRetention + pruneEvery + time.Minute)
	_ = o.Follow(ctx, "s2", "cred", "")
	_, _ = o.Report(ctx, "s2", mustReport(t, "new")) // this write sweeps

	got, _ := o.Events(ctx, "cred", 0, 0)
	if len(got.Events) != 1 || got.Events[0].SessionID != "s2" {
		t.Fatalf("after retention = %+v", got)
	}
}

// The route serves the polling credential's rows and no one else's.
func TestEventsRouteScopesByCredential(t *testing.T) {
	o, _ := newTestOutbox(t, nil)
	sessions := newFakeSessions()
	h := New(sessions, testProjects, WithSettings(Settings{AcceptActions: true}), WithOutbox(o))
	ctx := context.Background()

	_ = o.Follow(ctx, "s1", "cred-1", "")
	_, _ = o.Report(ctx, "s1", mustReport(t, "for cred-1"))

	rec := serve(t, h, peerRow("peer"), "GET", "/api/peer/events?since=0", "")
	if rec.Code != 200 {
		t.Fatalf("events = %d %s", rec.Code, rec.Body.String())
	}
	var out EventsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Events) != 1 {
		t.Fatalf("events = %+v", out)
	}

	other := peerRow("peer")
	other.ID.String = "cred-2"
	rec = serve(t, h, other, "GET", "/api/peer/events", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Events) != 0 {
		t.Fatalf("cred-2 read cred-1's rows: %+v", out)
	}

	for _, bad := range []string{"?since=-1", "?since=x", "?wait=-3"} {
		if rec := serve(t, h, peerRow("peer"), "GET", "/api/peer/events"+bad, ""); reasonOf(t, rec) != ReasonBadRequest {
			t.Errorf("%s = %d %s", bad, rec.Code, rec.Body.String())
		}
	}
}

// A machine-wide event goes to every paired server holding a peer credential,
// followed session or not.
func TestOutboxPublishReachesEveryPeerCredential(t *testing.T) {
	o, _ := newTestOutbox(t, nil)
	ctx := context.Background()
	q := o.store.(*store.Queries)
	for _, id := range []string{"cred-a", "cred-b"} {
		if _, err := q.CreateUser(ctx, store.CreateUserParams{ID: "u-" + id, DisplayName: id, IsAdmin: 1}); err != nil {
			t.Fatal(err)
		}
		if err := q.CreateAuthSession(ctx, store.CreateAuthSessionParams{TokenHash: "h-" + id,
			ID: sqlNull(id), UserID: "u-" + id, ExpiresAt: "2999-01-01T00:00:00Z", Kind: "peer"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := o.Publish(ctx, EventFinding, map[string]any{"kind": "disk-low", "opened": true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cred-a", "cred-b"} {
		got, _ := o.Events(ctx, id, 0, 0)
		if len(got.Events) != 1 || got.Events[0].Kind != EventFinding {
			t.Errorf("%s events = %+v", id, got)
		}
	}
}

func sqlNull(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
