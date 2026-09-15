package content

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenInRoot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := filepath.Join(base, "root")
	writeFile(t, filepath.Join(root, "nested", "shot.png"), "PNG")
	writeFile(t, filepath.Join(root, "notes..md"), "dots")
	writeFile(t, filepath.Join(base, "agentique.db"), "SECRET")
	if err := os.Symlink("nested", filepath.Join(root, "rel")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nested"), filepath.Join(root, "abs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "agentique.db"), filepath.Join(root, "out.png")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		rel  string
		body string
		err  error
	}{
		{rel: "nested/shot.png", body: "PNG"},
		{rel: "notes..md", body: "dots"},
		{rel: "rel/shot.png", body: "PNG"},
		{rel: "nested/../nested/shot.png", body: "PNG"},
		{rel: "missing.png", err: ErrNotFound},
		{rel: "nested", err: ErrNotFound},
		{rel: "", err: ErrInvalidPath},
		{rel: "../agentique.db", err: ErrInvalidPath},
		{rel: "/etc/hostname", err: ErrInvalidPath},
		{rel: "out.png", err: ErrInvalidPath},
		{rel: "abs/shot.png", err: ErrInvalidPath},
	}
	for _, c := range cases {
		item, err := OpenInRoot(root, c.rel, 0)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%q: err = %v, want %v", c.rel, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.rel, err)
			continue
		}
		got, _ := io.ReadAll(item.Body)
		item.Close()
		if string(got) != c.body {
			t.Errorf("%q: body = %q, want %q", c.rel, got, c.body)
		}
	}
}

func TestOpenInRootBoundsSize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big.txt"), strings.Repeat("x", 11))
	if _, err := OpenInRoot(root, "big.txt", 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// The header policy is the Item's name, whatever the body is — which is the
// property a relayed file depends on.
func TestServeJudgesByName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, wantType string
		download       bool
	}{
		{"shot.png", "image/png", false},
		{"page.html", "application/octet-stream", true},
		{"event-3-1", "application/octet-stream", true},
	} {
		rec := httptest.NewRecorder()
		Serve(rec, httptest.NewRequest(http.MethodGet, "/x", nil), FromBytes(c.name, []byte("<script>"), false))
		if got := rec.Header().Get("Content-Type"); got != c.wantType {
			t.Errorf("%s: Content-Type = %q, want %q", c.name, got, c.wantType)
		}
		if got := strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment"); got != c.download {
			t.Errorf("%s: attachment = %v, want %v", c.name, got, c.download)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no sandbox CSP", c.name)
		}
	}
}

func TestServeStreamsANonSeekableBody(t *testing.T) {
	t.Parallel()
	closed := false
	// Wrapped so the body is not an io.ReadSeeker, as a relayed response is not.
	body := struct{ io.Reader }{strings.NewReader("a,b")}
	item := NewItem("report.csv", 3, time.Time{}, body, func() error {
		closed = true
		return nil
	})
	rec := httptest.NewRecorder()
	Serve(rec, httptest.NewRequest(http.MethodGet, "/x", nil), item)
	if rec.Body.String() != "a,b" || rec.Header().Get("Content-Length") != "3" {
		t.Errorf("body %q length %q", rec.Body.String(), rec.Header().Get("Content-Length"))
	}
	if !closed {
		t.Error("Serve did not close the item")
	}
}

func TestRespondErrorStatuses(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]int{
		ErrNotFound:             http.StatusNotFound,
		ErrInvalidPath:          http.StatusBadRequest,
		ErrTooLarge:             http.StatusRequestEntityTooLarge,
		ErrUnavailable:          http.StatusBadGateway,
		ErrUnsupported:          http.StatusNotImplemented,
		errors.New("disk gone"): http.StatusInternalServerError,
	} {
		rec := httptest.NewRecorder()
		RespondError(rec, err)
		if rec.Code != want {
			t.Errorf("%v: status %d, want %d", err, rec.Code, want)
		}
		if strings.Contains(rec.Body.String(), "disk gone") {
			t.Error("an unclassified error's text reached the response")
		}
	}
}
