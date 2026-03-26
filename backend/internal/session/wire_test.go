package session

import (
	"encoding/json"
	"strings"
	"testing"

	claudecli "github.com/allbin/claudecli-go"
)

func TestToWireEvent_ToolResultTextOnly(t *testing.T) {
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_123",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "file contents here"},
		},
	}

	wire := ToWireEvent(event)
	tr, ok := wire.(WireToolResultEvent)
	if !ok {
		t.Fatalf("expected WireToolResultEvent, got %T", wire)
	}

	if tr.ToolID != "tu_123" {
		t.Errorf("ToolID = %q, want tu_123", tr.ToolID)
	}
	if len(tr.Content) != 1 {
		t.Fatalf("Content has %d blocks, want 1", len(tr.Content))
	}
	if tr.Content[0].Type != "text" || tr.Content[0].Text != "file contents here" {
		t.Errorf("block = %+v, want text block", tr.Content[0])
	}
}

func TestToWireEvent_ToolResultWithImage(t *testing.T) {
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_456",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "Screenshot saved"},
			{Type: "image", MediaType: "image/png", Data: "iVBORw0KGgo="},
		},
	}

	wire := ToWireEvent(event)
	tr, ok := wire.(WireToolResultEvent)
	if !ok {
		t.Fatalf("expected WireToolResultEvent, got %T", wire)
	}

	if len(tr.Content) != 2 {
		t.Fatalf("Content has %d blocks, want 2", len(tr.Content))
	}

	// Text block
	if tr.Content[0].Type != "text" || tr.Content[0].Text != "Screenshot saved" {
		t.Errorf("block[0] = %+v, want text", tr.Content[0])
	}

	// Image block — should be a data URL
	img := tr.Content[1]
	if img.Type != "image" {
		t.Errorf("block[1].Type = %q, want image", img.Type)
	}
	if img.MediaType != "image/png" {
		t.Errorf("block[1].MediaType = %q, want image/png", img.MediaType)
	}
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(img.URL, wantPrefix) {
		t.Errorf("block[1].URL doesn't start with %q: %q", wantPrefix, img.URL[:min(len(img.URL), 40)])
	}
}

func TestToWireEvent_ToolResultJSON(t *testing.T) {
	event := &claudecli.ToolResultEvent{
		ToolUseID: "tu_789",
		Content: []claudecli.ToolContent{
			{Type: "text", Text: "ok"},
			{Type: "image", MediaType: "image/png", Data: "AAAA"},
		},
	}

	wire := ToWireEvent(event)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the JSON structure matches what the frontend expects.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["type"] != "tool_result" {
		t.Errorf("type = %v", parsed["type"])
	}
	if parsed["toolId"] != "tu_789" {
		t.Errorf("toolId = %v", parsed["toolId"])
	}

	blocks, ok := parsed["content"].([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content = %v, want 2-element array", parsed["content"])
	}

	b0 := blocks[0].(map[string]any)
	if b0["type"] != "text" || b0["text"] != "ok" {
		t.Errorf("block[0] = %v", b0)
	}

	b1 := blocks[1].(map[string]any)
	if b1["type"] != "image" {
		t.Errorf("block[1].type = %v", b1["type"])
	}
	if b1["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("block[1].url = %v", b1["url"])
	}
}

func TestToolResultText(t *testing.T) {
	blocks := []WireContentBlock{
		{Type: "text", Text: "line1"},
		{Type: "image", URL: "data:image/png;base64,AAAA"},
		{Type: "text", Text: "line2"},
	}
	got := toolResultText(blocks)
	if got != "line1line2" {
		t.Errorf("toolResultText = %q, want line1line2", got)
	}
}
