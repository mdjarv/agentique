package mcphttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/devurls"
)

const slotConfigJSON = `[{"slot":"dev1","port":9210,"host":"dev1.example.com"}]`

func twoSlotStore() *devurls.Store {
	return devurls.NewStore([]config.DevURLSlot{
		{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
		{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
	})
}

// rpcCall hits the handler with a JSON-RPC POST and returns parsed result.
func rpcCall(t *testing.T, h http.Handler, token, method string, params any) (jsonrpcResponse, *http.Response) {
	t.Helper()
	body := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  method,
	}
	if params != nil {
		raw, _ := json.Marshal(params)
		body.Params = raw
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()

	bodyOut, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		t.Logf("status=%d body=%s", resp.StatusCode, bodyOut)
	}
	var parsed jsonrpcResponse
	if len(bodyOut) > 0 {
		_ = json.Unmarshal(bodyOut, &parsed)
	}
	return parsed, resp
}

func newTestHandler(t *testing.T) (http.Handler, *TokenStore, *devurls.Store) {
	t.Helper()
	tokens := NewTokenStore()
	store := twoSlotStore()
	h := NewHandler(tokens, store)
	return h, tokens, store
}

func TestHandler_RejectsMissingToken(t *testing.T) {
	h, _, _ := newTestHandler(t)
	_, resp := rpcCall(t, h, "", "initialize", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
	}
}

func TestHandler_RejectsBadToken(t *testing.T) {
	h, _, _ := newTestHandler(t)
	_, resp := rpcCall(t, h, "totally-bogus", "initialize", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
	}
}

func TestHandler_Initialize(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	got, resp := rpcCall(t, h, tok, "initialize", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got.Error != nil {
		t.Fatalf("rpc error: %+v", got.Error)
	}
	var result map[string]any
	_ = json.Unmarshal(got.Result, &result)
	if _, ok := result["protocolVersion"]; !ok {
		t.Error("initialize result missing protocolVersion")
	}
}

func TestHandler_NotificationReturns202(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	body := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d", w.Code)
	}
}

func TestHandler_GetReturns405(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

func TestHandler_ToolsList_IncludesAllTools(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	got, _ := rpcCall(t, h, tok, "tools/list", nil)
	if got.Error != nil {
		t.Fatalf("rpc error: %+v", got.Error)
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(got.Result, &result)

	want := map[string]bool{
		"SendMessage":   false,
		"AcquireDevUrl": false,
		"ReleaseDevUrl": false,
		"ListDevUrls":   false,
	}
	for _, tool := range result.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, present := range want {
		if !present {
			t.Errorf("tools/list missing %s", name)
		}
	}
}

func TestHandler_AcquireDevUrl_Success(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	got, _ := rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "AcquireDevUrl",
		"arguments": map[string]any{},
	})
	if got.Error != nil {
		t.Fatalf("rpc error: %+v", got.Error)
	}
	text := extractText(t, got)
	for _, expect := range []string{"dev1", "9210", "dev1.example.com", "https://dev1.example.com"} {
		if !strings.Contains(text, expect) {
			t.Errorf("response missing %q in: %s", expect, text)
		}
	}
}

func TestHandler_AcquireDevUrl_Exhausted(t *testing.T) {
	h, tokens, store := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")
	// Pre-fill all slots from other sessions.
	if _, err := store.Acquire("sess-X"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire("sess-Y"); err != nil {
		t.Fatal(err)
	}

	got, _ := rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "AcquireDevUrl",
		"arguments": map[string]any{},
	})
	// Spec says tool errors return content+isError, not jsonrpc error.
	if got.Error != nil {
		t.Fatalf("expected tool-level error in content, got rpc error %+v", got.Error)
	}
	if !isToolError(t, got) {
		t.Error("want isError=true")
	}
	text := extractText(t, got)
	if !strings.Contains(text, "busy") && !strings.Contains(text, "use") {
		t.Errorf("response should mention exhaustion: %s", text)
	}
}

func TestHandler_ReleaseDevUrl(t *testing.T) {
	h, tokens, store := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")
	if _, err := store.Acquire("sess-A"); err != nil {
		t.Fatal(err)
	}

	got, _ := rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "ReleaseDevUrl",
		"arguments": map[string]any{},
	})
	if got.Error != nil {
		t.Fatal(got.Error)
	}
	if len(store.List()) != 0 {
		t.Errorf("lease should be released, still have %d", len(store.List()))
	}
}

func TestHandler_ListDevUrls(t *testing.T) {
	h, tokens, store := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")
	_, _ = store.Acquire("sess-X")

	got, _ := rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "ListDevUrls",
		"arguments": map[string]any{},
	})
	if got.Error != nil {
		t.Fatal(got.Error)
	}
	text := extractText(t, got)
	if !strings.Contains(text, "dev1") || !strings.Contains(text, "dev2") {
		t.Errorf("expected both slots in list output: %s", text)
	}
	if !strings.Contains(text, "sess-X") {
		t.Errorf("expected holder shown in list: %s", text)
	}
}

func TestHandler_AcquireDevUrl_DerivesSessionFromToken(t *testing.T) {
	// Verifies that even if the model passed a fake sessionId in arguments,
	// the server uses the token's session.
	h, tokens, store := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	_, _ = rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "AcquireDevUrl",
		"arguments": map[string]any{"sessionId": "sess-EVIL"},
	})
	leases := store.List()
	if len(leases) != 1 {
		t.Fatalf("want 1 lease, got %d", len(leases))
	}
	if leases[0].SessionID != "sess-A" {
		t.Errorf("lease should belong to token's session, got %s", leases[0].SessionID)
	}
}

func TestHandler_UnknownTool(t *testing.T) {
	h, tokens, _ := newTestHandler(t)
	tok, _ := tokens.Mint("sess-A")

	got, _ := rpcCall(t, h, tok, "tools/call", map[string]any{
		"name":      "NoSuchTool",
		"arguments": map[string]any{},
	})
	if got.Error == nil {
		t.Errorf("want rpc error for unknown tool")
	}
}

// --- helpers ---

func extractText(t *testing.T, r jsonrpcResponse) string {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	var b strings.Builder
	for _, c := range result.Content {
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func isToolError(t *testing.T, r jsonrpcResponse) bool {
	t.Helper()
	var result struct {
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return result.IsError
}

// Sanity: helpers compile.
var _ = errors.New
var _ = slotConfigJSON
