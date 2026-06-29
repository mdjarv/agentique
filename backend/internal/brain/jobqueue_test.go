package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// --- in-memory fake jobStore (goroutine-safe) ---

type fakeJobStore struct {
	mu   sync.Mutex
	rows map[string]store.BrainJob
	seq  int64
}

func newFakeJobStore() *fakeJobStore { return &fakeJobStore{rows: map[string]store.BrainJob{}} }

func (f *fakeJobStore) CreateBrainJob(_ context.Context, arg store.CreateBrainJobParams) (store.BrainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	j := store.BrainJob{
		ID: arg.ID, Kind: arg.Kind, Scope: arg.Scope, Payload: arg.Payload, Attempts: arg.Attempts,
		CreatedAt: fmt.Sprintf("2026-01-01T00:00:%02dZ", f.seq),
	}
	f.rows[arg.ID] = j
	return j, nil
}

func (f *fakeJobStore) ListBrainJobs(_ context.Context) ([]store.BrainJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.BrainJob, 0, len(f.rows))
	for _, j := range f.rows {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeJobStore) UpdateBrainJobAttempts(_ context.Context, arg store.UpdateBrainJobAttemptsParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.rows[arg.ID]
	if !ok {
		return fmt.Errorf("no row %s", arg.ID)
	}
	j.Attempts = arg.Attempts
	j.LastError = arg.LastError
	f.rows[arg.ID] = j
	return nil
}

func (f *fakeJobStore) DeleteBrainJob(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeJobStore) get(id string) (store.BrainJob, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.rows[id]
	return j, ok
}

func (f *fakeJobStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// recordBus counts Broadcasts; satisfies eventbus.Broadcaster structurally.
type recordBus struct {
	mu         sync.Mutex
	broadcasts int
}

func (b *recordBus) Publish(topic, eventType string, payload any) {}
func (b *recordBus) Broadcast(eventType string, payload any) {
	b.mu.Lock()
	b.broadcasts++
	b.mu.Unlock()
}
func (b *recordBus) count() int { b.mu.Lock(); defer b.mu.Unlock(); return b.broadcasts }

// errCountHandler counts ERROR-level slog records (for the dead-letter assertion).
type errCountHandler struct {
	mu   sync.Mutex
	errs int
}

func (h *errCountHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *errCountHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelError {
		h.mu.Lock()
		h.errs++
		h.mu.Unlock()
	}
	return nil
}
func (h *errCountHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *errCountHandler) WithGroup(string) slog.Handler      { return h }
func (h *errCountHandler) count() int                         { h.mu.Lock(); defer h.mu.Unlock(); return h.errs }

func pollUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatal(msg)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// --- tests ---

func TestJobQueue_EnqueueDrainSuccess(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	bus := &recordBus{}
	done := make(chan Job, 1)
	q := NewJobQueue(db, bus, 5, map[string]JobHandler{
		JobKindLearn: func(_ context.Context, j Job) (bool, error) { done <- j; return true, nil },
	})

	events := []TranscriptEvent{{Type: "prompt", Data: `{"prompt":"hi"}`}}
	if err := q.Enqueue(ctx, JobKindLearn, "p1", events); err != nil {
		t.Fatal(err)
	}

	var got Job
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}
	if len(got.Events) != 1 || got.Events[0].Data != `{"prompt":"hi"}` || got.Events[0].Type != "prompt" {
		t.Fatalf("events not round-tripped: %+v", got.Events)
	}
	if got.Scope != ScopeForProject("p1") {
		t.Fatalf("scope=%s, want %s", got.Scope, ScopeForProject("p1"))
	}
	if got.ProjectID != "p1" {
		t.Fatalf("projectID=%s", got.ProjectID)
	}
	pollUntil(t, func() bool { return bus.count() == 1 }, "expected exactly one EventBrainUpdated broadcast")
	pollUntil(t, func() bool { return db.count() == 0 }, "job row not deleted after success")
}

func TestJobQueue_PayloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	q := NewJobQueue(db, nil, 5, map[string]JobHandler{}) // no handler → row retained for inspection

	events := []TranscriptEvent{{Type: "prompt", Data: `{"prompt":"a"}`}, {Type: "text", Data: `{"content":"b"}`}}
	if err := q.Enqueue(ctx, JobKindLearn, "proj", events); err != nil {
		t.Fatal(err)
	}
	rows, _ := db.ListBrainJobs(ctx)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != JobKindLearn {
		t.Fatalf("kind=%s", row.Kind)
	}
	if row.Scope != string(ScopeForProject("proj")) {
		t.Fatalf("scope=%s", row.Scope)
	}
	var p jobPayload
	if err := json.Unmarshal([]byte(row.Payload), &p); err != nil {
		t.Fatal(err)
	}
	if p.ProjectID != "proj" || len(p.Events) != 2 || p.Events[0].Data != `{"prompt":"a"}` || p.Events[1].Type != "text" {
		t.Fatalf("payload not byte-identical: %+v", p)
	}
}

func TestJobQueue_RetryThenDeadLetter(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	if _, err := db.CreateBrainJob(ctx, store.CreateBrainJobParams{
		ID: "j1", Kind: JobKindLearn, Scope: "global", Payload: `{"project_id":"","events":[]}`, Attempts: 0,
	}); err != nil {
		t.Fatal(err)
	}

	prev := slog.Default()
	ch := &errCountHandler{}
	slog.SetDefault(slog.New(ch))
	defer slog.SetDefault(prev)

	var calls int32
	q := NewJobQueue(db, nil, 3, map[string]JobHandler{
		JobKindLearn: func(_ context.Context, _ Job) (bool, error) {
			atomic.AddInt32(&calls, 1)
			return false, fmt.Errorf("boom")
		},
	})
	for i := 0; i < 5; i++ {
		q.Drain(ctx)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("handler invoked %d×, want 3 (maxAttempts)", n)
	}
	row, ok := db.get("j1")
	if !ok {
		t.Fatal("dead-lettered job must be retained, not deleted")
	}
	if row.Attempts != 3 {
		t.Fatalf("Attempts=%d, want 3", row.Attempts)
	}
	if row.LastError == "" {
		t.Fatal("LastError should be persisted")
	}
	if ch.count() != 1 {
		t.Fatalf("dead-letter ERROR logged %d×, want exactly 1", ch.count())
	}
}

func TestJobQueue_ResumeAfterRestart(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	payload, _ := json.Marshal(jobPayload{ProjectID: "p1", Events: []TranscriptEvent{{Type: "prompt", Data: "hi"}}})
	// Pre-seed a pending row as if left by a crash (Attempts already 1).
	if _, err := db.CreateBrainJob(ctx, store.CreateBrainJobParams{
		ID: "j1", Kind: JobKindLearn, Scope: "project:p1", Payload: string(payload), Attempts: 1,
	}); err != nil {
		t.Fatal(err)
	}

	ran := make(chan Job, 1)
	q := NewJobQueue(db, nil, 5, map[string]JobHandler{
		JobKindLearn: func(_ context.Context, j Job) (bool, error) { ran <- j; return true, nil },
	})
	q.Drain(ctx) // startup recovery

	select {
	case j := <-ran:
		if j.ProjectID != "p1" || len(j.Events) != 1 || j.Events[0].Data != "hi" {
			t.Fatalf("persisted payload not replayed correctly: %+v", j)
		}
	default:
		t.Fatal("pending job was not resumed on drain")
	}
	if _, ok := db.get("j1"); ok {
		t.Fatal("resumed job should be deleted after success")
	}
}

func TestJobQueue_UnknownKindSkipped(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	if _, err := db.CreateBrainJob(ctx, store.CreateBrainJobParams{
		ID: "j1", Kind: JobKindOutcome, Scope: "global", Payload: `{}`, Attempts: 0,
	}); err != nil {
		t.Fatal(err)
	}
	q := NewJobQueue(db, nil, 5, map[string]JobHandler{
		JobKindLearn: func(_ context.Context, _ Job) (bool, error) { return false, nil }, // no outcome handler
	})
	q.Drain(ctx)

	row, ok := db.get("j1")
	if !ok {
		t.Fatal("unknown-kind job must be retained")
	}
	if row.Attempts != 0 {
		t.Fatalf("Attempts=%d, want 0 (no attempt burned for a disabled kind)", row.Attempts)
	}
}

func TestJobQueue_SingleFlight(t *testing.T) {
	ctx := context.Background()
	db := newFakeJobStore()
	if _, err := db.CreateBrainJob(ctx, store.CreateBrainJobParams{
		ID: "A", Kind: JobKindLearn, Scope: "global", Payload: `{"project_id":"","events":[]}`, Attempts: 0,
	}); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	seen := map[string]int{}
	blockA := make(chan struct{})
	q := NewJobQueue(db, nil, 5, map[string]JobHandler{
		JobKindLearn: func(_ context.Context, j Job) (bool, error) {
			if j.ID == "A" {
				<-blockA // hold the active pass open
			}
			mu.Lock()
			seen[j.ID]++
			mu.Unlock()
			return true, nil
		},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); q.Drain(ctx) }()
	go func() { defer wg.Done(); q.Drain(ctx) }()
	// Enqueue B while A's handler is blocked — it must be picked up by the follow-up pass.
	if err := q.Enqueue(ctx, JobKindLearn, "p2", nil); err != nil {
		t.Fatal(err)
	}
	close(blockA)
	wg.Wait()

	pollUntil(t, func() bool { return db.count() == 0 }, "the job enqueued mid-pass was stranded (no follow-up pass)")
	mu.Lock()
	defer mu.Unlock()
	if seen["A"] != 1 {
		t.Fatalf("job A handled %d×, want exactly 1 under single-flight", seen["A"])
	}
}

func TestBrainJobStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)

	for _, id := range []string{"j2", "j1"} { // insert out of order to prove ORDER BY
		if _, err := q.CreateBrainJob(ctx, store.CreateBrainJobParams{
			ID: id, Kind: JobKindLearn, Scope: "global", Payload: `{"k":1}`, Attempts: 0,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := q.ListBrainJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "j1" || rows[1].ID != "j2" {
		t.Fatalf("ListBrainJobs order wrong (want created_at ASC, id ASC): %+v", rows)
	}

	if err := q.UpdateBrainJobAttempts(ctx, store.UpdateBrainJobAttemptsParams{Attempts: 2, LastError: "boom", ID: "j1"}); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteBrainJob(ctx, "j2"); err != nil {
		t.Fatal(err)
	}
	rows, _ = q.ListBrainJobs(ctx)
	if len(rows) != 1 || rows[0].ID != "j1" || rows[0].Attempts != 2 || rows[0].LastError != "boom" {
		t.Fatalf("round-trip failed: %+v", rows)
	}
}
