package session

import (
	"context"
	"testing"

	"github.com/allbin/agentkit/runtime"
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
