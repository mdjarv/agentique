package brain

import (
	"strings"
	"testing"
)

func TestBuildTranscriptExtractsContent(t *testing.T) {
	events := []TranscriptEvent{
		{Type: "prompt", Data: `{"prompt":"how do I build?"}`},
		{Type: "thinking", Data: `{"content":"hmm"}`}, // omitted
		{Type: "text", Data: `{"type":"text","content":"Use just, not npx."}`},
		{Type: "tool_use", Data: `{"name":"Bash"}`}, // omitted
		{Type: "agent_result", Data: `{"content":[{"type":"text","text":"Found the auth flow."}]}`},
		{Type: "user_message", Data: `{"content":"thanks"}`},
	}
	chunks := BuildTranscript(events, 0)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	tr := chunks[0]
	for _, want := range []string{"User: how do I build?", "Assistant: Use just, not npx.", "Subagent: Found the auth flow.", "User: thanks"} {
		if !strings.Contains(tr, want) {
			t.Errorf("transcript missing %q:\n%s", want, tr)
		}
	}
	if strings.Contains(tr, "hmm") || strings.Contains(tr, "Bash") {
		t.Errorf("noise event types must be omitted:\n%s", tr)
	}
}

func TestBuildTranscriptEmpty(t *testing.T) {
	if chunks := BuildTranscript([]TranscriptEvent{{Type: "tool_use", Data: `{}`}}, 0); chunks != nil {
		t.Fatalf("no content events should yield nil, got %v", chunks)
	}
}

func TestBuildTranscriptChunks(t *testing.T) {
	long := strings.Repeat("x", 100)
	var events []TranscriptEvent
	for i := 0; i < 10; i++ {
		events = append(events, TranscriptEvent{Type: "text", Data: `{"content":"` + long + `"}`})
	}
	chunks := BuildTranscript(events, 250)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > 250 {
			t.Fatalf("chunk %d exceeds max: %d chars", i, len(c))
		}
	}
}
