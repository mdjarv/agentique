package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/allbin/agentkit/eventbus"
	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Durable retry queue (brain-evolution Band 1 M7). Session-end learning/outcome passes used to
// run in a bare `go onSessionEnd(...)` goroutine — a restart mid-LLM-extraction silently lost the
// work. This replaces that with a durable, at-least-once, idempotent queue backed by the
// brain_jobs table: enqueue on session end, drain on startup + on enqueue, retry with a bounded
// budget, then dead-letter (retain the row + ERROR log) — never silent loss. store does not
// import brain (one-directional), so the queue depends on store via a narrow interface.
const (
	JobKindLearn          = "learn"
	JobKindOutcome        = "outcome"
	defaultJobMaxAttempts = 5
)

// jobStore is the narrow slice of *store.Queries the queue needs (kept an interface so tests use
// an in-memory fake and the queue never depends on the concrete store).
type jobStore interface {
	CreateBrainJob(ctx context.Context, arg store.CreateBrainJobParams) (store.BrainJob, error)
	ListBrainJobs(ctx context.Context) ([]store.BrainJob, error)
	UpdateBrainJobAttempts(ctx context.Context, arg store.UpdateBrainJobAttemptsParams) error
	DeleteBrainJob(ctx context.Context, id string) error
}

// Job is a decoded brain job handed to a JobHandler.
type Job struct {
	ID        string
	Kind      string
	Scope     memory.Scope
	ProjectID string
	Events    []TranscriptEvent
	Attempts  int
}

// JobHandler runs one job. It returns whether it changed the brain (to drive a single
// EventBrainUpdated broadcast per drain pass) and an error (which retries the job).
type JobHandler func(ctx context.Context, j Job) (changed bool, err error)

// jobPayload is the JSON persisted in the brain_jobs.payload column.
type jobPayload struct {
	ProjectID string            `json:"project_id"`
	Events    []TranscriptEvent `json:"events"`
}

// JobQueue is the durable, single-process drainer.
type JobQueue struct {
	db          jobStore
	bus         eventbus.Broadcaster
	handlers    map[string]JobHandler
	maxAttempts int

	mu       sync.Mutex
	draining bool
	requeued bool
}

// NewJobQueue builds a queue. maxAttempts<=0 uses defaultJobMaxAttempts.
func NewJobQueue(db jobStore, bus eventbus.Broadcaster, maxAttempts int, handlers map[string]JobHandler) *JobQueue {
	if maxAttempts <= 0 {
		maxAttempts = defaultJobMaxAttempts
	}
	return &JobQueue{db: db, bus: bus, handlers: handlers, maxAttempts: maxAttempts}
}

// Enqueue durably persists a job (the insert is synchronous — the job is durable before Enqueue
// returns) and kicks a background drain.
func (q *JobQueue) Enqueue(ctx context.Context, kind, projectID string, events []TranscriptEvent) error {
	payload, err := json.Marshal(jobPayload{ProjectID: projectID, Events: events})
	if err != nil {
		return fmt.Errorf("brain: marshal job payload: %w", err)
	}
	if _, err := q.db.CreateBrainJob(ctx, store.CreateBrainJobParams{
		ID:       uuid.NewString(),
		Kind:     kind,
		Scope:    string(ScopeForProject(projectID)),
		Payload:  string(payload),
		Attempts: 0,
	}); err != nil {
		return fmt.Errorf("brain: enqueue %s job: %w", kind, err)
	}
	go q.Drain(context.Background())
	return nil
}

// Drain processes all pending jobs. Single-flight with a follow-up pass: a concurrent Enqueue that
// arrives mid-drain sets requeued so its job is not stranded until the next session end.
func (q *JobQueue) Drain(ctx context.Context) {
	q.mu.Lock()
	if q.draining {
		q.requeued = true
		q.mu.Unlock()
		return
	}
	q.draining = true
	q.mu.Unlock()
	for {
		q.drainPass(ctx)
		q.mu.Lock()
		if !q.requeued {
			q.draining = false
			q.mu.Unlock()
			return
		}
		q.requeued = false
		q.mu.Unlock()
	}
}

// drainPass runs at most one attempt per job. Success deletes the row; an error increments
// attempts and persists last_error; at maxAttempts the row is dead-lettered (kept, ERROR-logged,
// excluded from future passes). A kind with no registered handler (model disabled this run) is
// skipped without burning an attempt.
func (q *JobQueue) drainPass(ctx context.Context) {
	jobs, err := q.db.ListBrainJobs(ctx)
	if err != nil {
		slog.Warn("brain: drain list jobs failed", "error", err)
		return
	}
	changedAny := false
	for _, row := range jobs {
		if int(row.Attempts) >= q.maxAttempts {
			continue // dead-lettered
		}
		handler := q.handlers[row.Kind]
		if handler == nil {
			continue // model disabled this run; resume when re-enabled, no attempt burned
		}
		var payload jobPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			q.bumpAttempt(ctx, row, fmt.Errorf("decode payload: %w", err))
			continue
		}
		changed, herr := handler(ctx, Job{
			ID:        row.ID,
			Kind:      row.Kind,
			Scope:     memory.Scope(row.Scope),
			ProjectID: payload.ProjectID,
			Events:    payload.Events,
			Attempts:  int(row.Attempts),
		})
		if herr != nil {
			q.bumpAttempt(ctx, row, herr)
			continue
		}
		if err := q.db.DeleteBrainJob(ctx, row.ID); err != nil {
			slog.Warn("brain: delete completed job failed", "id", row.ID, "error", err)
			continue
		}
		changedAny = changedAny || changed
	}
	if changedAny && q.bus != nil {
		q.bus.Broadcast(EventBrainUpdated, map[string]string{})
	}
}

func (q *JobQueue) bumpAttempt(ctx context.Context, row store.BrainJob, cause error) {
	attempts := int(row.Attempts) + 1
	if err := q.db.UpdateBrainJobAttempts(ctx, store.UpdateBrainJobAttemptsParams{
		Attempts:  int64(attempts),
		LastError: cause.Error(),
		ID:        row.ID,
	}); err != nil {
		slog.Warn("brain: update job attempts failed", "id", row.ID, "error", err)
		return
	}
	if attempts >= q.maxAttempts {
		slog.Error("brain: job dead-lettered", "id", row.ID, "kind", row.Kind, "attempts", attempts, "error", cause)
	}
}
