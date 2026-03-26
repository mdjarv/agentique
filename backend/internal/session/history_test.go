package session

import (
	"encoding/json"
	"testing"
)

func mustUnmarshal(t *testing.T, data json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func TestNormalizeEventJSON_OldToolUse(t *testing.T) {
	old := `{"type":"tool_use","id":"1","name":"Read","input":{}}`
	got := mustUnmarshal(t, normalizeEventJSON("tool_use", []byte(old)))

	if got["toolId"] != "1" {
		t.Errorf("toolId = %v, want 1", got["toolId"])
	}
	if got["toolName"] != "Read" {
		t.Errorf("toolName = %v, want Read", got["toolName"])
	}
	if got["toolInput"] == nil {
		t.Error("toolInput is nil")
	}
	if _, ok := got["id"]; ok {
		t.Error("old key 'id' still present")
	}
	if _, ok := got["name"]; ok {
		t.Error("old key 'name' still present")
	}
	if _, ok := got["input"]; ok {
		t.Error("old key 'input' still present")
	}
}

func TestNormalizeEventJSON_OldToolResult(t *testing.T) {
	old := `{"type":"tool_result","toolUseId":"1","content":"ok"}`
	got := mustUnmarshal(t, normalizeEventJSON("tool_result", []byte(old)))

	if got["toolId"] != "1" {
		t.Errorf("toolId = %v, want 1", got["toolId"])
	}
	// String content should be migrated to array format.
	blocks, ok := got["content"].([]any)
	if !ok {
		t.Fatalf("content is %T, want []any", got["content"])
	}
	if len(blocks) != 1 {
		t.Fatalf("content has %d blocks, want 1", len(blocks))
	}
	block := blocks[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "ok" {
		t.Errorf("content block = %v, want {type:text, text:ok}", block)
	}
	if _, ok := got["toolUseId"]; ok {
		t.Error("old key 'toolUseId' still present")
	}
}

func TestNormalizeEventJSON_AlreadyNormalized(t *testing.T) {
	toolUse := `{"type":"tool_use","toolId":"1","toolName":"Read","toolInput":{}}`
	got := normalizeEventJSON("tool_use", []byte(toolUse))
	if string(got) != toolUse {
		t.Errorf("tool_use modified: %s", got)
	}

	// tool_result with string content should be migrated to array format.
	toolResult := `{"type":"tool_result","toolId":"1","content":"ok"}`
	trGot := mustUnmarshal(t, normalizeEventJSON("tool_result", []byte(toolResult)))
	blocks, ok := trGot["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Errorf("tool_result content not migrated to array: %v", trGot["content"])
	}

	// tool_result with array content should pass through.
	arrayContent := `{"type":"tool_result","toolId":"1","content":[{"type":"text","text":"ok"}]}`
	got = normalizeEventJSON("tool_result", []byte(arrayContent))
	if string(got) != arrayContent {
		t.Errorf("tool_result with array content modified: %s", got)
	}
}

func TestNormalizeEventJSON_OtherEvents(t *testing.T) {
	cases := []struct {
		typ  string
		data string
	}{
		{"text", `{"type":"text","content":"hello"}`},
		{"thinking", `{"type":"thinking","content":"hmm"}`},
		{"result", `{"type":"result","costUsd":0.01}`},
	}
	for _, tc := range cases {
		got := normalizeEventJSON(tc.typ, []byte(tc.data))
		if string(got) != tc.data {
			t.Errorf("%s: modified: %s", tc.typ, got)
		}
	}
}

func TestNormalizeEventJSON_MalformedJSON(t *testing.T) {
	bad := `{not valid json`
	got := normalizeEventJSON("tool_use", []byte(bad))
	if string(got) != bad {
		t.Errorf("malformed JSON modified: %s", got)
	}
}
