package assistant

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// hangingDirectory is a directory whose create — and, when asked, whose project
// list — does not answer until the test releases it, whatever its context says.
// That is the verb that never answers: the one a turn used to wait on for its
// whole budget.
type hangingDirectory struct {
	*fakeDirectory
	hangList bool

	release   chan struct{}
	cancelled chan struct{}
	once      sync.Once
}

func newHangingDirectory(projects []ProjectRow) *hangingDirectory {
	return &hangingDirectory{
		fakeDirectory: &fakeDirectory{projects: projects},
		release:       make(chan struct{}),
		cancelled:     make(chan struct{}),
	}
}

func (d *hangingDirectory) CreateSession(ctx context.Context, projectID, model string) (SessionRow, error) {
	d.hang(ctx)
	return d.fakeDirectory.CreateSession(ctx, projectID, model)
}

func (d *hangingDirectory) ListProjects(ctx context.Context) []ProjectRow {
	if d.hangList {
		d.hang(ctx)
	}
	return d.fakeDirectory.ListProjects(ctx)
}

func (d *hangingDirectory) hang(ctx context.Context) {
	go func() {
		<-ctx.Done()
		d.once.Do(func() { close(d.cancelled) })
	}()
	<-d.release
}

// syncBuffer is a log sink a test can read while the service writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A verb that never answers is refused at its own deadline, the turn goes on to
// a reply, and the verb's context is cancelled so its IO stops. A verb that
// writes cannot say nothing happened — it may have — so the words say the
// outcome is unknown and tell the head to look before anything else.
func TestAWriteVerbThatNeverAnswersIsRefusedAsUnknown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := newHangingDirectory([]ProjectRow{{ID: "p1", Name: "riff", Reach: ReachLocal}})
	t.Cleanup(func() { close(dir.release) })

	var answered map[string]any
	head := &workingHead{reply: "It may have gone through; let me check.", work: func(ctx context.Context, svc *Service, _ func(string)) {
		answered = svc.ToolHandler(ctx, VerbCreateSession, map[string]any{"project": "riff", "prompt": "fix it"})
	}}
	logs := &syncBuffer{}
	svc, _, _ := newTestService(t, WithHeadManager(head), WithDirectory(dir), WithDispatcher(&fakeDispatcher{}),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	svc.verbBudget = 50 * time.Millisecond
	head.svc = svc

	started := time.Now()
	if _, err := svc.Say(ctx, SurfaceThread, "make a session in riff"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the turn took %s; the verb's deadline was 50ms", took)
	}

	said, _ := answered["error"].(string)
	if !strings.Contains(said, "UNKNOWN") || !strings.Contains(said, "do not try it again") {
		t.Errorf("answer = %q, want the outcome called unknown and a retry ruled out", said)
	}
	if _, leaked := answered[reasonKey]; leaked {
		t.Errorf("the answer carries its internal reason: %v", answered)
	}

	select {
	case <-dir.cancelled:
	case <-time.After(3 * time.Second):
		t.Error("the verb's context was never cancelled, so its IO does not stop at the deadline")
	}

	reply, ok := stepsOf(t, svc)
	if !ok {
		t.Fatal("no reply stored: the turn did not survive the verb")
	}
	if reply.Text != "It may have gone through; let me check." {
		t.Errorf("reply = %q, want the head's own words", reply.Text)
	}
	if len(reply.Steps) != 1 {
		t.Fatalf("steps = %+v, want the one verb", reply.Steps)
	}
	step := reply.Steps[0]
	if step.Status != StepFailed || !strings.Contains(step.Outcome, "did not answer within") {
		t.Errorf("step = %+v, want it failed with the timeout sentence", step)
	}
	if !strings.Contains(logs.String(), "reason=verb-timed-out:create_session") {
		t.Errorf("the refusal was not logged with its reason; log:\n%s", logs.String())
	}
}

// A verb that only reads can say plainly that there is no answer.
func TestAReadVerbThatNeverAnswersSaysThereIsNoAnswer(t *testing.T) {
	t.Parallel()
	dir := newHangingDirectory([]ProjectRow{{ID: "p1", Name: "riff"}})
	dir.hangList = true
	t.Cleanup(func() { close(dir.release) })

	svc, _, _ := newTestService(t, WithDirectory(dir))
	svc.verbBudget = 50 * time.Millisecond

	answered := svc.ToolHandler(context.Background(), VerbListProjects, map[string]any{})
	said, _ := answered["error"].(string)
	if !strings.Contains(said, "no answer to give") {
		t.Errorf("answer = %q, want it to say there is no answer", said)
	}
	if strings.Contains(said, "UNKNOWN") {
		t.Errorf("answer = %q: a read has no outcome to be unknown about", said)
	}
}

// A verb that answers after its deadline has its answer logged, because for a
// verb that writes that line is the only record of whether it went through.
func TestAVerbThatAnswersLateIsLogged(t *testing.T) {
	t.Parallel()
	dir := newHangingDirectory([]ProjectRow{{ID: "p1", Name: "riff", Reach: ReachLocal}})
	logs := &syncBuffer{}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	svc.verbBudget = 50 * time.Millisecond

	svc.ToolHandler(context.Background(), VerbCreateSession, map[string]any{"project": "riff"})
	close(dir.release)

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(logs.String(), "assistant verb answered after its deadline") {
		if time.Now().After(deadline) {
			t.Fatalf("the late answer was never logged; log:\n%s", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "verb=create_session") {
		t.Errorf("the late answer's log line does not name the verb; log:\n%s", logs.String())
	}
}

// The caller going away is not the verb being slow: nothing is told it timed
// out, because nobody is left to tell.
func TestACancelledCallIsNotCalledATimeout(t *testing.T) {
	t.Parallel()
	dir := newHangingDirectory([]ProjectRow{{ID: "p1", Name: "riff"}})
	dir.hangList = true
	t.Cleanup(func() { close(dir.release) })
	svc, _, _ := newTestService(t, WithDirectory(dir))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	answered := svc.ToolHandler(ctx, VerbListProjects, map[string]any{})
	if said, _ := answered["error"].(string); strings.Contains(said, "did not answer within") {
		t.Errorf("answer = %q, want a cancelled call not reported as a timeout", said)
	}
}
