package peer

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

type fakeContent struct{ files session.LocalContent }

func (f fakeContent) SessionFile(ctx context.Context, id, rel string) (content.Item, error) {
	return f.files.SessionFile(ctx, id, rel)
}

func (fakeContent) EventImage(_ context.Context, _ string, eventID int64, idx int) (content.Item, error) {
	if eventID != 7 || idx != 0 {
		return content.Item{}, content.ErrNotFound
	}
	return content.FromBytes(session.EventImageName(eventID, idx, "image/png"), []byte("PNG"), true), nil
}

func contentHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, worktreeSession), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, worktreeSession, "my report.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(newFakeSessions(), testProjects, WithContent(fakeContent{files: session.LocalContent{FilesDir: dir}}))
}

func TestContentServesAPeerAndNamesTheItem(t *testing.T) {
	t.Parallel()
	h := contentHandler(t)
	rec := serve(t, h, peerRow(auth.KindPeer), http.MethodGet,
		"/api/peer/sessions/"+worktreeSession+"/files?path="+url.QueryEscape("my report.md"), "")
	if rec.Code != http.StatusOK || rec.Body.String() != "# hi" {
		t.Fatalf("file = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(ContentNameHeader); got != url.PathEscape("my report.md") {
		t.Errorf("%s = %q", ContentNameHeader, got)
	}

	rec = serve(t, h, peerRow(auth.KindPeer), http.MethodGet,
		"/api/peer/sessions/"+worktreeSession+"/events/7/images/0", "")
	if rec.Code != http.StatusOK || rec.Header().Get(ContentNameHeader) != "event-7-0.png" {
		t.Fatalf("image = %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
}

func TestContentRefusals(t *testing.T) {
	t.Parallel()
	h := contentHandler(t)
	cases := []struct {
		name   string
		row    *store.GetAuthSessionRow
		target string
		status int
		reason string
	}{
		{"browser credential", peerRow("bearer"), "/api/peer/sessions/" + worktreeSession + "/files?path=x.md", http.StatusForbidden, ReasonNotPeer},
		{"unknown session", peerRow(auth.KindPeer), "/api/peer/sessions/00000000-0000-4000-8000-0000000000ee/files?path=x.md", http.StatusNotFound, ReasonNotFound},
		{"missing file", peerRow(auth.KindPeer), "/api/peer/sessions/" + worktreeSession + "/files?path=nope.md", http.StatusNotFound, ReasonNotFound},
		{"traversal", peerRow(auth.KindPeer), "/api/peer/sessions/" + worktreeSession + "/files?path=..%2F..%2Fagentique.db", http.StatusBadRequest, ReasonBadRequest},
		{"no path", peerRow(auth.KindPeer), "/api/peer/sessions/" + worktreeSession + "/files", http.StatusBadRequest, ReasonBadRequest},
		{"bad index", peerRow(auth.KindPeer), "/api/peer/sessions/" + worktreeSession + "/events/7/images/-1", http.StatusBadRequest, ReasonBadRequest},
	}
	for _, c := range cases {
		rec := serve(t, h, c.row, http.MethodGet, c.target, "")
		if rec.Code != c.status || reasonOf(t, rec) != c.reason {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		if rec.Header().Get(ContentNameHeader) != "" {
			t.Errorf("%s: a refusal carried the content name header", c.name)
		}
	}
}

func TestContentRoutesAbsentWithoutASource(t *testing.T) {
	t.Parallel()
	h := New(newFakeSessions(), testProjects)
	rec := serve(t, h, peerRow(auth.KindPeer), http.MethodGet, "/api/peer/sessions/"+worktreeSession+"/files?path=x.md", "")
	if rec.Code != http.StatusNotFound || reasonOf(t, rec) != "" {
		t.Fatalf("without WithContent = %d %s", rec.Code, rec.Body.String())
	}
}
