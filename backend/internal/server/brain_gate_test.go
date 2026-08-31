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

// serveWithBrain stands up a server with the brain master switch in a given position.
// A disabled brain must mount nothing at all, so these tests probe the route table
// rather than a handler's response body.
//
// Note an unmounted /api/ path does NOT 404: it falls through to the SPA handler and
// comes back as text/html (see CLAUDE.md, "A `{id}` route parameter is not one path
// segment" — requiresAuth covers the /api/ prefix, everything else is an SPA asset).
// So "the route is absent" is asserted as "the answer is not JSON", which is what
// actually separates a mounted brain handler from the catch-all.
func serveWithBrain(t *testing.T, enabled bool) *httptest.Server {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		t.Fatalf("run migrations: %v", err)
	}

	cfg := server.Config{DB: db, BrainEnabled: enabled}
	if enabled {
		cfg.BrainDir = filepath.Join(t.TempDir(), "brain")
	}
	srv, err := server.New(store.New(db), cfg)
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

// brainFeature reads the "brain" entry out of /api/health's features map. That map is
// the only thing telling the SPA whether the Brain destination exists, so a surface
// that reads the brain can check it instead of rendering and failing.
func brainFeature(t *testing.T, ts *httptest.Server) bool {
	t.Helper()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	return body.Features["brain"]
}

// contentType reports what answered a path, which is how a mounted API handler is told
// apart from the SPA catch-all.
func contentType(t *testing.T, ts *httptest.Server, path string) string {
	t.Helper()

	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type")
}

// The switch defaults off, and off means the subsystem was never built: no handler is
// registered, so the request reaches the SPA catch-all instead of a memory list.
func TestBrainDisabledByDefault(t *testing.T) {
	ts := serveWithBrain(t, false)

	if ct := contentType(t, ts, "/api/brain/memories"); strings.Contains(ct, "json") {
		t.Errorf("brain route answered %q with the switch off; want the SPA catch-all", ct)
	}
	if brainFeature(t, ts) {
		t.Error(`health features["brain"] = true with the switch off`)
	}
}

// Enabled restores the whole surface: the handler answers JSON. What it says about an
// empty store is the brain package's business, not the gate's.
func TestBrainEnabledMountsRoutes(t *testing.T) {
	ts := serveWithBrain(t, true)

	if ct := contentType(t, ts, "/api/brain/memories"); !strings.Contains(ct, "json") {
		t.Errorf("brain route answered %q with the switch on; want the mounted handler", ct)
	}
	if !brainFeature(t, ts) {
		t.Error(`health features["brain"] = false with the switch on`)
	}
}

// BrainDir names the store, so it is still required: enabled with no directory builds
// nothing, and must report that honestly rather than advertising a tab that 404s.
func TestBrainEnabledWithoutDirStaysOff(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	srv, err := server.New(store.New(db), server.Config{DB: db, BrainEnabled: true})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	if brainFeature(t, ts) {
		t.Error(`health features["brain"] = true with no BrainDir`)
	}
}
