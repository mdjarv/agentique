package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/session"
)

const (
	localContentSession  = "30000000-0000-4000-8000-0000000000a1"
	remoteContentSession = "30000000-0000-4000-8000-0000000000b1"
)

// fakeRemoteContent is an owner that names every item the way it likes and
// records what it was asked.
type fakeRemoteContent struct {
	name  string
	body  string
	err   error
	asked []string
}

func (f *fakeRemoteContent) SessionFile(_ context.Context, machineID, sessionID, rel string) (content.Item, error) {
	f.asked = append(f.asked, machineID+" "+sessionID+" "+rel)
	if f.err != nil {
		return content.Item{}, f.err
	}
	return content.NewItem(f.name, int64(len(f.body)), time.Time{}, strings.NewReader(f.body), nil), nil
}

func (f *fakeRemoteContent) EventImage(_ context.Context, machineID, sessionID string, eventID int64, idx int) (content.Item, error) {
	f.asked = append(f.asked, machineID+" "+sessionID)
	if f.err != nil {
		return content.Item{}, f.err
	}
	return content.NewItem(f.name, int64(len(f.body)), time.Time{}, strings.NewReader(f.body), nil), nil
}

func contentRoutes(t *testing.T, remote *fakeRemoteContent) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, localContentSession), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, localContentSession, "local.md"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := &sessionContent{
		local:   session.LocalContent{FilesDir: dir},
		isLocal: func(_ context.Context, id string) (bool, error) { return id == localContentSession, nil },
		locate: func(_ context.Context, id string) (string, bool) {
			return "zbook", id == remoteContentSession
		},
		remote: remote,
	}
	h := &session.ContentHandler{Source: src}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", h.HandleFile)
	mux.HandleFunc("GET /api/sessions/{id}/events/{eventId}/images/{idx}", h.HandleEventImage)
	return mux
}

func get(mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestSessionContentServesLocalAndRelaysRemote(t *testing.T) {
	t.Parallel()
	remote := &fakeRemoteContent{name: "report.pdf", body: "%PDF"}
	mux := contentRoutes(t, remote)

	if rec := get(mux, "/api/sessions/"+localContentSession+"/files/local.md"); rec.Code != http.StatusOK || rec.Body.String() != "here" {
		t.Fatalf("local = %d %q", rec.Code, rec.Body.String())
	}
	if len(remote.asked) != 0 {
		t.Fatalf("a local session asked the remote: %v", remote.asked)
	}

	rec := get(mux, "/api/sessions/"+remoteContentSession+"/files/out/report.pdf")
	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF" {
		t.Fatalf("remote = %d %q", rec.Code, rec.Body.String())
	}
	if len(remote.asked) != 1 || remote.asked[0] != "zbook "+remoteContentSession+" out/report.pdf" {
		t.Fatalf("asked = %v", remote.asked)
	}

	if rec := get(mux, "/api/sessions/30000000-0000-4000-8000-0000000000ff/files/x.md"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session = %d", rec.Code)
	}
}

// The owner does not choose how a relayed file renders on this origin: the
// name comes from the path this server was asked for.
func TestSessionContentRelayedHeadersAreThisServers(t *testing.T) {
	t.Parallel()
	remote := &fakeRemoteContent{name: "shot.png", body: "<script>alert(1)</script>"}
	mux := contentRoutes(t, remote)

	rec := get(mux, "/api/sessions/"+remoteContentSession+"/files/page.html")
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want a download whatever the owner named it", ct)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") ||
		rec.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Errorf("headers = %v", rec.Header())
	}

	// An event image's extension is the owner's, read through the allowlist.
	remote.name = "event-3-0.html"
	rec = get(mux, "/api/sessions/"+remoteContentSession+"/events/3/images/0")
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("image Content-Type = %q", ct)
	}
}

func TestSessionContentRefusesBeforeAsking(t *testing.T) {
	t.Parallel()
	remote := &fakeRemoteContent{name: "x", body: "x"}
	mux := contentRoutes(t, remote)
	for _, target := range []string{
		"/api/sessions/" + remoteContentSession + "/files/..%2F..%2Fagentique.db",
		"/api/sessions/..%2F..%2F/files/x.md",
	} {
		if rec := get(mux, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", target, rec.Code)
		}
	}
	if len(remote.asked) != 0 {
		t.Fatalf("a bad name reached the owner: %v", remote.asked)
	}
}

func TestSessionContentRelayFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"owner says not found", &peerlink.RefusalError{Status: 404, Reason: peer.ReasonNotFound}, http.StatusNotFound},
		{"owner says bad path", &peerlink.RefusalError{Status: 400, Reason: peer.ReasonBadRequest}, http.StatusBadRequest},
		{"older release", peerlink.ErrNoPeerSurface, http.StatusNotImplemented},
		{"machine down", errors.New("dial tcp: connection refused"), http.StatusBadGateway},
		{"too large", content.ErrTooLarge, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		mux := contentRoutes(t, &fakeRemoteContent{err: c.err})
		rec := get(mux, "/api/sessions/"+remoteContentSession+"/files/x.md")
		if rec.Code != c.want {
			t.Errorf("%s = %d, want %d", c.name, rec.Code, c.want)
		}
		if body, _ := io.ReadAll(rec.Body); strings.Contains(string(body), "connection refused") {
			t.Errorf("%s: transport detail reached the response: %s", c.name, body)
		}
	}
}
