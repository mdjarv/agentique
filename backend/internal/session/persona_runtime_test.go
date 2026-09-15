package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// The persona's own thinking reaches onThought, encrypted blocks included; a
// subagent's does not, and a text delta still goes to onText.
func TestPersonaForwardsItsOwnThoughtsOnly(t *testing.T) {
	t.Parallel()
	var thoughts []string
	var texts []string
	p := &sessionlessPersona{
		onText:    func(delta string) { texts = append(texts, delta) },
		onThought: func(text string) { thoughts = append(thoughts, text) },
	}
	ctx := context.Background()

	p.onEvent(ctx, runtime.ThinkingEvent{Content: "", Signature: "sig"})
	p.onEvent(ctx, runtime.ThinkingEvent{Content: "check the list first"})
	p.onEvent(ctx, runtime.ThinkingEvent{Content: "a subagent's", ParentToolUseID: "toolu_1"})
	p.onEvent(ctx, runtime.AssistantTextDeltaEvent{Delta: "Two sessions"})

	if len(thoughts) != 2 || thoughts[0] != "" || thoughts[1] != "check the list first" {
		t.Errorf("thoughts = %q, want the encrypted block and the clear one, and no subagent's", thoughts)
	}
	if len(texts) != 1 || texts[0] != "Two sessions" {
		t.Errorf("texts = %q, want the delta", texts)
	}
}

// A persona nobody asked to report thoughts ignores them.
func TestPersonaWithoutOnThoughtIgnoresThinking(t *testing.T) {
	t.Parallel()
	p := &sessionlessPersona{}
	p.onEvent(context.Background(), runtime.ThinkingEvent{Content: "x"})
}

// paramsConnector hands out mock CLI sessions and keeps what each Connect was
// asked for, so a test can raise what the real adapter raises through those
// params — a question, an approval — from the CLI's side.
type paramsConnector struct {
	mu     sync.Mutex
	params []runtime.ConnectParams
	cli    []*testutil.MockCLISession
}

func (c *paramsConnector) Connect(_ context.Context, p runtime.ConnectParams) (runtime.CLISession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cli := testutil.NewMockCLISession()
	c.params = append(c.params, p)
	c.cli = append(c.cli, cli)
	return cli, nil
}

func (c *paramsConnector) last() (runtime.ConnectParams, *testutil.MockCLISession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.params[len(c.params)-1], c.cli[len(c.cli)-1]
}

// startTestPersona starts a sessionless persona over conn and runs one Query in
// the background, answering the result channel once it returns.
func startTestPersona(t *testing.T, conn *paramsConnector) (personaRuntime, <-chan personaTurnEnd) {
	t.Helper()
	mgr := NewManager(nil, nil, nil, &paramsConnector{})
	mgr.SetPersonaConnector(PersonaToolsWeb, conn)
	rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{WorkDir: t.TempDir(), Tools: PersonaToolsWeb})
	if err != nil {
		t.Fatalf("start persona: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	result := make(chan personaTurnEnd, 1)
	go func() {
		text, err := rt.Query(context.Background(), "which one?")
		result <- personaTurnEnd{text: text, err: err}
	}()
	_, cli := conn.last()
	deadline := time.Now().Add(3 * time.Second)
	for len(cli.Queries()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the persona never sent its query")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return rt, result
}

// A question has nobody to answer it on a persona, so it is refused the moment
// it is raised and the turn goes on — rather than parking until the caller's
// budget runs out, which is what the assistant's head did on 2026-09-15.
func TestPersonaRefusesAQuestionAtOnce(t *testing.T) {
	t.Parallel()
	conn := &paramsConnector{}
	_, result := startTestPersona(t, conn)
	params, cli := conn.last()

	asked := make(chan error, 1)
	go func() {
		_, err := params.UserInput(context.Background(), []runtime.Question{{
			Question: "Which Agentique should the new session run on?",
			Options:  []runtime.QuestionOption{{Label: "This machine"}, {Label: "zbook"}},
		}})
		asked <- err
	}()
	select {
	case err := <-asked:
		if err == nil {
			t.Fatal("the question was answered; nobody could have answered it")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the question parked the turn; it should have been refused at once")
	}

	if err := cli.Inject(runtime.TurnCompletedEvent{Status: runtime.TurnStatusCompleted, Text: "Which machine?"}); err != nil {
		t.Fatalf("inject completion: %v", err)
	}
	select {
	case end := <-result:
		if end.err != nil || end.text != "Which machine?" {
			t.Errorf("query = %q, %v; want the reply the turn ended with", end.text, end.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("query did not return after the turn completed")
	}
}

// A CLI that dies mid-turn ends the Query with it. No completion can arrive
// from a process that is gone, so waiting for one only spends the caller's
// budget.
func TestPersonaQueryEndsWhenTheCLIDies(t *testing.T) {
	t.Parallel()
	conn := &paramsConnector{}
	_, result := startTestPersona(t, conn)
	_, cli := conn.last()

	if err := cli.Close(); err != nil {
		t.Fatalf("close cli: %v", err)
	}
	select {
	case end := <-result:
		if end.err == nil {
			t.Errorf("query = %q with no error; a turn whose CLI died did not complete", end.text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("query kept waiting on a CLI that had already died")
	}
}
