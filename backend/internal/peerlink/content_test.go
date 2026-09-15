package peerlink

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

	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func ownerWithFiles(t *testing.T, files map[string]string) *owner {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, remoteSession, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return newOwnerWith(t, true, &ownerSessions{}, noProjects{},
		peer.WithContent(session.LocalContent{FilesDir: dir}))
}

func TestClientStreamsASessionFile(t *testing.T) {
	o := ownerWithFiles(t, map[string]string{"out/my report.md": "# report"})
	c := New(o.server.Client(), actingCatalog(t, o))

	item, err := c.SessionFile(context.Background(), ownerMachineID, remoteSession, "out/my report.md")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer item.Close()
	body, _ := io.ReadAll(item.Body)
	if string(body) != "# report" || item.Name != "my report.md" || item.Size != int64(len("# report")) {
		t.Fatalf("item = %q name %q size %d", body, item.Name, item.Size)
	}
}

func TestClientContentRefusals(t *testing.T) {
	o := ownerWithFiles(t, nil)
	c := New(o.server.Client(), actingCatalog(t, o))
	ctx := context.Background()

	_, err := c.SessionFile(ctx, ownerMachineID, remoteSession, "missing.png")
	var refusal *RefusalError
	if !errors.As(err, &refusal) || refusal.Reason != peer.ReasonNotFound {
		t.Errorf("missing = %v, want a not-found refusal", err)
	}
	_, err = c.SessionFile(ctx, ownerMachineID, remoteSession, "../../agentique.db")
	if !errors.As(err, &refusal) || refusal.Reason != peer.ReasonBadRequest {
		t.Errorf("traversal = %v, want a bad-request refusal", err)
	}
}

// A release with a peer surface but no content routes reads as a machine that
// cannot relay, and so does one answering the route with its SPA's 200 — those
// bytes are an HTML page, not the file somebody linked.
func TestClientContentFromAnOlderRelease(t *testing.T) {
	o := newOwner(t, true)
	c := New(o.server.Client(), actingCatalog(t, o))
	if _, err := c.SessionFile(context.Background(), ownerMachineID, remoteSession, "x.md"); !errors.Is(err, ErrNoPeerSurface) {
		t.Errorf("no route = %v, want ErrNoPeerSurface", err)
	}

	real := o.server.Config.Handler
	spa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/files") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><div id=root></div>"))
			return
		}
		real.ServeHTTP(w, r)
	}))
	t.Cleanup(spa.Close)
	catalog := actingCatalog(t, o)
	if err := catalog.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: ownerMachineID, Label: "zbook", BaseUrl: spa.URL, Token: o.bearer,
		AddedAt: "2026-09-14T00:00:00Z", IdentityKey: o.key,
	}); err != nil {
		t.Fatal(err)
	}
	c = New(spa.Client(), catalog)
	_, err := c.SessionFile(context.Background(), ownerMachineID, remoteSession, "x.md")
	if !errors.Is(err, ErrNoPeerSurface) || !strings.Contains(err.Error(), "without naming") {
		t.Errorf("SPA answer = %v, want ErrNoPeerSurface from the missing name", err)
	}
}

func TestBoundedBodyFailsPastTheBound(t *testing.T) {
	t.Parallel()
	exact := &boundedBody{r: stringsReader("abc"), left: 3}
	if b, err := io.ReadAll(exact); err != nil || string(b) != "abc" {
		t.Errorf("exact = %q, %v", b, err)
	}
	over := &boundedBody{r: stringsReader("abcd"), left: 3}
	if _, err := io.ReadAll(over); !errors.Is(err, content.ErrTooLarge) {
		t.Errorf("over = %v, want ErrTooLarge", err)
	}
}

func stringsReader(s string) io.Reader { return &onlyReader{s: s} }

type onlyReader struct{ s string }

func (r *onlyReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}
