package mcphttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/devurls"
)

// TestFullSessionLifecycle exercises the flow a spawned Claude subprocess
// would run: initialize → notifications/initialized → tools/list → tools/call
// (Acquire) → tools/call (Release).
func TestFullSessionLifecycle(t *testing.T) {
	tokens := NewTokenStore()
	store := devurls.NewStoreWithProbes([]config.DevURLSlot{
		{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
	},
		func(int) error { return nil },
		func(int) (*devurls.PortOwner, error) { return nil, nil },
	)
	h := NewHandler(tokens, store, nil)

	ts := httptest.NewServer(h)
	defer ts.Close()

	tok, _ := tokens.Mint("sess-lifecycle")

	post := func(body []byte) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// 1. initialize
	status, body := post([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if status != 200 {
		t.Fatalf("initialize status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "protocolVersion") {
		t.Errorf("initialize missing protocolVersion: %s", body)
	}

	// 2. notifications/initialized → 202
	status, _ = post([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if status != http.StatusAccepted {
		t.Errorf("notifications/initialized: want 202, got %d", status)
	}

	// 3. tools/list
	status, body = post([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if status != 200 {
		t.Fatalf("tools/list status=%d body=%s", status, body)
	}
	for _, name := range []string{"SendMessage", "AcquireDevUrl", "ReleaseDevUrl", "ListDevUrls"} {
		if !strings.Contains(string(body), `"name":"`+name+`"`) {
			t.Errorf("tools/list missing %s: %s", name, body)
		}
	}

	// 4. tools/call AcquireDevUrl
	status, body = post([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"AcquireDevUrl","arguments":{}}}`))
	if status != 200 {
		t.Fatalf("acquire status=%d body=%s", status, body)
	}
	if !strings.Contains(string(body), "dev1.example.com") {
		t.Errorf("acquire response missing host: %s", body)
	}

	// Confirm store reflects the lease.
	leases := store.List()
	if len(leases) != 1 || leases[0].SessionID != "sess-lifecycle" {
		t.Errorf("store should have 1 lease for sess-lifecycle, got %+v", leases)
	}

	// 5. tools/call ReleaseDevUrl
	status, body = post([]byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ReleaseDevUrl","arguments":{}}}`))
	if status != 200 {
		t.Fatalf("release status=%d body=%s", status, body)
	}
	if len(store.List()) != 0 {
		t.Errorf("store should be empty after release")
	}

	// Revoke token → subsequent requests 401.
	tokens.Revoke("sess-lifecycle")
	status, _ = post([]byte(`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{}}`))
	if status != http.StatusUnauthorized {
		t.Errorf("after revoke: want 401, got %d", status)
	}
}

// TestCrossSessionIsolation verifies that sessions cannot read or release
// each other's leases via the MCP.
func TestCrossSessionIsolation(t *testing.T) {
	tokens := NewTokenStore()
	store := devurls.NewStoreWithProbes([]config.DevURLSlot{
		{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
		{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
	},
		func(int) error { return nil },
		func(int) (*devurls.PortOwner, error) { return nil, nil },
	)
	h := NewHandler(tokens, store, nil)

	tokA, _ := tokens.Mint("sess-A")
	tokB, _ := tokens.Mint("sess-B")

	acquire := func(tok string) string {
		t.Helper()
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"AcquireDevUrl","arguments":{}}}`)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		b, _ := io.ReadAll(w.Result().Body)
		return string(b)
	}

	_ = acquire(tokA)
	_ = acquire(tokB)

	// Each session should hold one distinct slot.
	leases := store.List()
	if len(leases) != 2 {
		t.Fatalf("want 2 leases, got %d", len(leases))
	}
	holders := map[string]string{}
	for _, l := range leases {
		holders[l.SessionID] = l.Slot
	}
	if holders["sess-A"] == holders["sess-B"] || holders["sess-A"] == "" || holders["sess-B"] == "" {
		t.Errorf("sessions should hold different slots, got %+v", holders)
	}

	// sess-A releasing should only free sess-A's slot.
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ReleaseDevUrl","arguments":{}}}`)))
	req.Header.Set("Authorization", "Bearer "+tokA)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	after := store.List()
	if len(after) != 1 || after[0].SessionID != "sess-B" {
		t.Errorf("expected only sess-B to remain, got %+v", after)
	}
}

// TestInvalidJSONReturns400 — protocol misuse should fail cleanly, not panic.
func TestInvalidJSONReturns400(t *testing.T) {
	tokens := NewTokenStore()
	store := devurls.NewStore(nil)
	h := NewHandler(tokens, store, nil)

	tok, _ := tokens.Mint("sess-A")
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte("not json at all")))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestMalformedParamsReturnsRpcError(t *testing.T) {
	tokens := NewTokenStore()
	store := devurls.NewStore(nil)
	h := NewHandler(tokens, store, nil)

	tok, _ := tokens.Mint("sess-A")
	// params is not an object — unmarshal into toolCallParams fails.
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"nope"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp jsonrpcResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error == nil {
		t.Error("want jsonrpc error for malformed params")
	}
}
