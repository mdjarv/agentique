package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The assistant's gate, on the brain's precedent: off means UNBUILT, and
// `features.assistant` is what tells the SPA so.
//
// It matters more here than a missing route would suggest. An unmounted /api/
// path does not 404 — it falls through to the SPA and answers text/html with a
// 200 — so a nav row added without that check leads somewhere that looks alive
// and is not. In M1 the assistant has no HTTP route of its own at all (it is
// reached over the socket), which makes the flag the ONLY thing a client can
// read, and therefore the whole of the gate as far as the frontend is
// concerned.

// serveWithAssistant stands up a server with the assistant switch in a given
// position.
func serveWithAssistant(t *testing.T, enabled bool) *httptest.Server {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		t.Fatalf("run migrations: %v", err)
	}

	srv, err := server.New(store.New(db), server.Config{DB: db, ExperimentalAssistant: enabled})
	if err != nil {
		db.Close()
		t.Fatalf("create server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		db.Close()
	})
	return ts
}

// feature reads one entry out of a JSON boolean map at path.
func feature(t *testing.T, ts *httptest.Server, path, block, name string) bool {
	t.Helper()

	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var flags map[string]bool
	if err := json.Unmarshal(body[block], &flags); err != nil {
		t.Fatalf("decode %s %s: %v", path, block, err)
	}
	return flags[name]
}

// The switch defaults off, and a client reading the flag never navigates to a
// surface the server does not serve.
func TestAssistantDisabledByDefault(t *testing.T) {
	ts := serveWithAssistant(t, false)

	if feature(t, ts, "/api/health", "features", "assistant") {
		t.Error(`health features["assistant"] = true with the switch off`)
	}
	// A paired machine reads the same fact from the descriptor, because a client
	// talks to several servers at once and each runs whatever release it
	// happens to be on.
	if feature(t, ts, "/.well-known/agentique/environment", "capabilities", "assistant") {
		t.Error(`descriptor capabilities["assistant"] = true with the switch off`)
	}
}

// On, the server says so on both surfaces that report what it can do.
func TestAssistantEnabledReportsItself(t *testing.T) {
	ts := serveWithAssistant(t, true)

	if !feature(t, ts, "/api/health", "features", "assistant") {
		t.Error(`health features["assistant"] = false with the switch on`)
	}
	if !feature(t, ts, "/.well-known/agentique/environment", "capabilities", "assistant") {
		t.Error(`descriptor capabilities["assistant"] = false with the switch on`)
	}
}

// The head's verbs are on an endpoint of their own, with a token store of their
// own, because tools/list is not scoped to the caller: one shared endpoint hands
// the whole table — the uncontained tier included — to every coding session on
// every turn.
//
// It exists exactly with the service. Off, the path is not registered at all and
// falls through to the SPA, which is what an unmounted path does here.
func TestTheHeadsMCPEndpointExistsWithTheService(t *testing.T) {
	on := serveWithAssistant(t, true)
	resp, err := http.Post(on.URL+"/mcp/assistant", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post /mcp/assistant: %v", err)
	}
	defer resp.Body.Close()
	// Unauthorized, not 404: the endpoint is there and refused an unknown
	// bearer, which is the only thing that reaches it.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /mcp/assistant = %d, want 401 from the head's own token store", resp.StatusCode)
	}

	off := serveWithAssistant(t, false)
	resp2, err := http.Post(off.URL+"/mcp/assistant", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post /mcp/assistant: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusUnauthorized {
		t.Error("the head's endpoint is mounted with the assistant switched off")
	}
}
