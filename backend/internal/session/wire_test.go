package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestToWireEvent_ErrorClassification(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantErrorType  string
		wantRetryAfter int
	}{
		{
			name:          "rate limit with retry",
			err:           &claudecli.RateLimitError{RetryAfter: 30 * time.Second, Message: "slow down"},
			wantErrorType: "rate_limit",
			wantRetryAfter: 30,
		},
		{
			name:          "rate limit without retry",
			err:           &claudecli.RateLimitError{Message: "slow down"},
			wantErrorType: "rate_limit",
		},
		{
			name:          "auth error",
			err:           claudecli.ErrAuth,
			wantErrorType: "auth",
		},
		{
			name:          "overloaded",
			err:           claudecli.ErrOverloaded,
			wantErrorType: "overloaded",
		},
		{
			name:          "ErrAPI wrapped",
			err:           fmt.Errorf("%w: billing_error: payment required", claudecli.ErrAPI),
			wantErrorType: "api_error",
		},
		{
			name:          "non-sentinel error",
			err:           errors.New("connection reset"),
			wantErrorType: "api_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &claudecli.ErrorEvent{Err: tt.err, Fatal: false}
			wire := ToWireEvent(event)
			we, ok := wire.(WireErrorEvent)
			if !ok {
				t.Fatalf("expected WireErrorEvent, got %T", wire)
			}
			if we.ErrorType != tt.wantErrorType {
				t.Errorf("ErrorType = %q, want %q", we.ErrorType, tt.wantErrorType)
			}
			if we.RetryAfterSecs != tt.wantRetryAfter {
				t.Errorf("RetryAfterSecs = %d, want %d", we.RetryAfterSecs, tt.wantRetryAfter)
			}
		})
	}
}

func TestToWireEvent_ResultContextWindowFromModelUsage(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		ModelUsage: map[string]claudecli.ModelUsage{
			"claude-opus-4-6": {
				InputTokens:       5_000,
				OutputTokens:      2_000,
				CacheReadTokens:   90_000,
				CacheCreateTokens: 5_000,
				ContextWindow:     200_000,
			},
		},
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	// InputTokens/OutputTokens should be 0 — enriched from stream data, not ModelUsage.
	if r.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0 (cumulative ModelUsage should not be used)", r.InputTokens)
	}
	if r.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", r.OutputTokens)
	}
	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000", r.ContextWindow)
	}
}

func TestToWireEvent_ResultDefaultContextWindow(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		Usage: claudecli.Usage{
			InputTokens:  50_000,
			OutputTokens: 3_000,
		},
		// ModelUsage is nil — should default to 200k
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000 (default fallback)", r.ContextWindow)
	}
}

func TestToWireEvent_ResultMultiModelMaxContextWindow(t *testing.T) {
	event := &claudecli.ResultEvent{
		CostUSD:    0.01,
		Duration:   time.Second,
		StopReason: "end_turn",
		ModelUsage: map[string]claudecli.ModelUsage{
			"claude-opus-4-6": {
				ContextWindow: 200_000,
			},
			"claude-haiku-4-5": {
				ContextWindow: 200_000,
			},
		},
	}

	wire := ToWireEvent(event)
	r, ok := wire.(WireResultEvent)
	if !ok {
		t.Fatalf("expected WireResultEvent, got %T", wire)
	}

	if r.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000 (max)", r.ContextWindow)
	}
}

func TestExtractStreamContextTokens(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{
			name: "message_start with all token types",
			json: `{"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":23174,"cache_creation_input_tokens":4083}}}`,
			want: 9 + 23174 + 4083,
		},
		{
			name: "message_start without cache fields",
			json: `{"type":"message_start","message":{"usage":{"input_tokens":150000}}}`,
			want: 150_000,
		},
		{
			name: "message_delta ignored",
			json: `{"type":"message_delta","usage":{"output_tokens":500}}`,
			want: 0,
		},
		{
			name: "content_block_start ignored",
			json: `{"type":"content_block_start","content_block":{"type":"text"}}`,
			want: 0,
		},
		{
			name: "invalid json",
			json: `{invalid`,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStreamContextTokens(json.RawMessage(tt.json))
			if got != tt.want {
				t.Errorf("extractStreamContextTokens = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractStreamOutputTokens(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{
			name: "message_delta with output_tokens",
			json: `{"type":"message_delta","usage":{"output_tokens":997}}`,
			want: 997,
		},
		{
			name: "message_start ignored",
			json: `{"type":"message_start","message":{"usage":{"input_tokens":150000}}}`,
			want: 0,
		},
		{
			name: "invalid json",
			json: `{invalid`,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStreamOutputTokens(json.RawMessage(tt.json))
			if got != tt.want {
				t.Errorf("extractStreamOutputTokens = %d, want %d", got, tt.want)
			}
		})
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
