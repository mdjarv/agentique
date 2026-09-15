package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// workingHead is a head whose turn does something before it answers: it calls
// verbs through the same door the MCP endpoint uses, and reports thoughts the
// way the runtime does.
type workingHead struct {
	svc   *Service
	reply string
	err   error
	work  func(ctx context.Context, svc *Service, think func(string))

	mu        sync.Mutex
	onThought func(string)
}

func (h *workingHead) StartHead(_ context.Context, p HeadParams) (HeadRuntime, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onThought = p.OnThought
	return h, nil
}

func (h *workingHead) Query(ctx context.Context, _ string) (string, error) {
	h.mu.Lock()
	think := h.onThought
	h.mu.Unlock()
	if think == nil {
		think = func(string) {}
	}
	if h.work != nil {
		h.work(ctx, h.svc, think)
	}
	return h.reply, h.err
}

func (h *workingHead) Close() error { return nil }

func stepsOf(t *testing.T, svc *Service) (Message, bool) {
	t.Helper()
	page, err := svc.History(context.Background(), "", 20)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	for i := len(page.Messages) - 1; i >= 0; i-- {
		if page.Messages[i].Role == RoleAssistant {
			return page.Messages[i], true
		}
	}
	return Message{}, false
}

// A turn's reply carries what the head did to reach it, in the order it did
// it, read back from the store rather than from the push.
func TestAReplyCarriesTheTurnsStepsInOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := &fakeMemory{found: []Fact{
		{ID: "f1", Text: "Release: tag first,\nthen the GitHub release", Scope: "project:p1", Source: "operator"},
		{ID: "f2", Text: "Never push unless asked", Scope: "global"},
	}}
	head := &workingHead{reply: "Tag first.", work: func(ctx context.Context, svc *Service, think func(string)) {
		think("")
		svc.ToolHandler(ctx, VerbRecall, map[string]any{"query": "release process"})
		svc.ToolHandler(ctx, VerbRecall, map[string]any{"query": "   "})
	}}
	svc, _, recorder := newTestService(t, WithHeadManager(head), WithMemory(mem))
	head.svc = svc

	if _, err := svc.Say(ctx, SurfaceThread, "how do we release?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	reply, ok := stepsOf(t, svc)
	if !ok {
		t.Fatal("no reply stored")
	}
	if len(reply.Steps) != 3 {
		t.Fatalf("reply carries %d steps, want a thought and two verbs: %+v", len(reply.Steps), reply.Steps)
	}

	thought, recall, refused := reply.Steps[0], reply.Steps[1], reply.Steps[2]
	if thought.Kind != StepThought || !thought.Encrypted || thought.Seq != 1 {
		t.Errorf("first step = %+v, want the encrypted thought, seq 1", thought)
	}

	if recall.Kind != StepVerb || recall.Verb != VerbRecall || recall.Status != StepDone {
		t.Errorf("second step = %+v, want a settled recall", recall)
	}
	if recall.Detail != "release process" {
		t.Errorf("recall detail = %q, want the query it looked for", recall.Detail)
	}
	if recall.Outcome != "2 facts" {
		t.Errorf("recall outcome = %q, want the facts counted", recall.Outcome)
	}
	if len(recall.Facts) != 2 || recall.Facts[0].ID != "f1" || recall.Facts[0].Scope != "project:p1" {
		t.Fatalf("recall facts = %+v, want both, with ids and scope", recall.Facts)
	}
	if recall.Facts[0].Text != "Release: tag first, then the GitHub release" {
		t.Errorf("fact text = %q, want it on one line", recall.Facts[0].Text)
	}

	if refused.Status != StepRefused || refused.Seq != 3 {
		t.Errorf("third step = %+v, want the refusal, seq 3", refused)
	}
	if !strings.Contains(refused.Outcome, "Nothing to look for") {
		t.Errorf("refusal outcome = %q, want the sentence the head was told", refused.Outcome)
	}
	if strings.Contains(refused.Outcome, "empty-recall-query") {
		t.Errorf("refusal outcome leaks the internal reason token: %q", refused.Outcome)
	}

	// A verb pushes twice, running then settled; a thought once.
	var pushes []Step
	for _, event := range recorder.Events() {
		if event.Type != EventStep {
			continue
		}
		push, ok := event.Payload.(StepPush)
		if !ok || push.Step == nil {
			t.Fatalf("step push payload = %#v", event.Payload)
		}
		if push.Surface != SurfaceThread {
			t.Errorf("step pushed for surface %q, want the thread", push.Surface)
		}
		pushes = append(pushes, *push.Step)
	}
	if len(pushes) != 5 {
		t.Fatalf("pushed %d steps, want 5 (thought, recall running+done, refusal running+settled)", len(pushes))
	}
	if pushes[1].Status != StepRunning || pushes[1].Seq != 2 {
		t.Errorf("second push = %+v, want recall running before it answers", pushes[1])
	}
}

// A turn that fails keeps what it did on the note that says it failed, and a
// verb that never answered is not stored as still running.
func TestAFailedTurnKeepsItsStepsOnTheNote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	head := &workingHead{err: errors.New("context deadline exceeded"), work: func(ctx context.Context, svc *Service, _ func(string)) {
		svc.ToolHandler(ctx, VerbListSessions, map[string]any{"filter": FilterRunning})
		// A verb the head asked for whose answer never came back before the
		// turn was cut off: begun, never settled.
		svc.beginVerbStep(VerbSummarizeSession, map[string]any{"session_id": "s-123"})
	}}
	svc, _, _ := newTestService(t, WithHeadManager(head), WithDirectory(&fakeDirectory{}))
	head.svc = svc

	if _, err := svc.Say(ctx, SurfaceThread, "what is running?"); err == nil {
		t.Fatal("Say() succeeded on a failed turn")
	}

	note, ok := stepsOf(t, svc)
	if !ok || note.Text != turnFailedText {
		t.Fatalf("stored = %+v, want the failure note", note)
	}
	if len(note.Steps) != 2 {
		t.Fatalf("note carries %d steps, want both: %+v", len(note.Steps), note.Steps)
	}
	if note.Steps[0].Verb != VerbListSessions || note.Steps[0].Detail != FilterRunning {
		t.Errorf("first step = %+v", note.Steps[0])
	}
	cut := note.Steps[1]
	if cut.Status != StepFailed || cut.SessionID != "s-123" || cut.Detail != "" {
		t.Errorf("unanswered step = %+v, want failed, the session carried apart from the detail", cut)
	}
}

// Outside a turn there is no recorder: a verb call is answered and nothing is
// kept or pushed.
func TestAVerbOutsideATurnRecordsNothing(t *testing.T) {
	t.Parallel()
	svc, _, recorder := newTestService(t, WithMemory(&fakeMemory{}))

	svc.ToolHandler(context.Background(), VerbRecall, map[string]any{"query": "anything"})

	for _, event := range recorder.Events() {
		if event.Type == EventStep {
			t.Fatalf("a verb outside a turn pushed a step: %+v", event.Payload)
		}
	}
	if rec, _ := svc.currentSteps(); rec != nil {
		t.Fatal("a recorder exists between turns")
	}
}

// A turn keeps a bounded number of steps and counts the rest.
func TestATurnKeepsAtMostItsCapAndCountsTheRest(t *testing.T) {
	t.Parallel()
	const calls = maxTurnSteps + 6
	head := &workingHead{reply: "done", work: func(ctx context.Context, svc *Service, _ func(string)) {
		for i := range calls {
			svc.ToolHandler(ctx, "no_such_verb", map[string]any{"query": fmt.Sprint(i)})
		}
	}}
	svc, _, _ := newTestService(t, WithHeadManager(head))
	head.svc = svc

	if _, err := svc.Say(context.Background(), SurfaceThread, "go"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	reply, _ := stepsOf(t, svc)
	if len(reply.Steps) != maxTurnSteps || reply.StepsOmitted != calls-maxTurnSteps {
		t.Fatalf("kept %d, omitted %d; want %d and %d", len(reply.Steps), reply.StepsOmitted, maxTurnSteps, calls-maxTurnSteps)
	}
	// An unknown verb is the table refusing, not a handler failing.
	if reply.Steps[0].Status != StepRefused {
		t.Errorf("unknown verb status = %q, want refused", reply.Steps[0].Status)
	}
}

// A heartbeat turn that only called verbs writes no reply, so its steps go
// onto the system message that woke it.
func TestASilentHeartbeatTurnKeepsItsStepsOnTheWakeUp(t *testing.T) {
	t.Parallel()
	head := &workingHead{work: func(ctx context.Context, svc *Service, _ func(string)) {
		svc.ToolHandler(ctx, VerbListSessions, map[string]any{"filter": FilterRecent})
	}}
	triager := &fakeTriager{answer: "act: nightly tests — the retry fix finished and its tests pass"}
	svc, _, _ := heartbeatWorld(t, triager, WithHeadManager(head), WithDirectory(&fakeDirectory{}))
	head.svc = svc
	somethingHappened(t, svc)

	if _, err := svc.Heartbeat(context.Background()); err != nil {
		t.Fatalf("Heartbeat() = %v", err)
	}

	page, err := svc.History(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Role != RoleSystem {
		t.Fatalf("conversation = %+v, want the wake-up alone", page.Messages)
	}
	woke := page.Messages[0]
	if woke.Kind != messageKindHeartbeat {
		t.Errorf("wake-up kind = %q, attaching steps must keep it", woke.Kind)
	}
	if len(woke.Steps) != 1 || woke.Steps[0].Verb != VerbListSessions {
		t.Fatalf("wake-up steps = %+v, want the verb the turn called", woke.Steps)
	}
}

// Rows written before steps existed carry a flat string map, and still read.
func TestAMessageWrittenBeforeStepsStillReads(t *testing.T) {
	t.Parallel()
	svc, queries, _ := newTestService(t)
	channelID, err := svc.EnsureConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = queries.InsertAssistantMessage(context.Background(), insertParams(channelID,
		`{"surface":"voice","callId":"c-1","kind":"digest"}`))
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.History(context.Background(), "", 5)
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("History() = %+v, %v", page, err)
	}
	msg := page.Messages[0]
	if msg.Surface != "voice" || msg.CallID != "c-1" || msg.Kind != "digest" || msg.Steps != nil {
		t.Errorf("old row read as %+v", msg)
	}
}

func TestStepOutcomeCountsTheAnswersOwnList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		payload map[string]any
		refusal bool
		want    string
	}{
		{map[string]any{"sessions": []any{1, 2}, "note": "x"}, false, "2 sessions"},
		{map[string]any{"sessions": []map[string]any{{}}}, false, "1 session"},
		{map[string]any{"facts": []any{}, "note": "Nothing on record"}, false, "nothing found"},
		{map[string]any{"proposal": "p-1"}, false, "done"},
		{map[string]any{"error": "Nothing to look for.\nSay what."}, true, "Nothing to look for. Say what."},
	}
	for _, c := range cases {
		if got := stepOutcome(c.payload, c.refusal); got != c.want {
			t.Errorf("stepOutcome(%v) = %q, want %q", c.payload, got, c.want)
		}
	}
}

func insertParams(channelID, metadata string) store.InsertAssistantMessageParams {
	return store.InsertAssistantMessageParams{
		ID:          "m-old",
		ChannelID:   channelID,
		SenderType:  senderPersona,
		Content:     "an old digest",
		MessageType: messageTypeMessage,
		Metadata:    metadata,
		CreatedAt:   "2026-09-01T00:00:00.000000000Z",
	}
}
