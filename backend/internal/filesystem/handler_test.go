package filesystem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/project"
)

// The windows shape ("C:\" as the first crumb) cannot run under a linux test
// binary — filepath switches on GOOS — but the walk-up loop is shared, so this
// pins its contract: root first as its own crumb, every crumb navigable,
// termination at the point where Dir stops changing the path.
func TestPathSegments(t *testing.T) {
	segs := pathSegments("/home/u/git/repo")
	want := []segment{
		{Name: "/", Path: "/"},
		{Name: "home", Path: "/home"},
		{Name: "u", Path: "/home/u"},
		{Name: "git", Path: "/home/u/git"},
		{Name: "repo", Path: "/home/u/git/repo"},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments %v, want %d", len(segs), segs, len(want))
	}
	for i, w := range want {
		if segs[i] != w {
			t.Errorf("segment %d: got %+v, want %+v", i, segs[i], w)
		}
	}

	root := pathSegments("/")
	if len(root) != 1 || root[0].Path != "/" {
		t.Errorf("root should be exactly its own crumb, got %v", root)
	}
}

// Validation says what an existing directory would be as a project, by the
// same repository-root rule the project list uses, and nothing for a path with
// no directory to judge.
func TestHandleValidateKind(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		path string
		want project.Kind
	}{
		"repository root":            {repo, project.KindGit},
		"folder inside a repository": {sub, project.KindFolder},
		"missing path":               {filepath.Join(repo, "new"), ""},
	}
	h := &Handler{}
	for name, tc := range cases {
		rec := httptest.NewRecorder()
		h.HandleValidate(rec, httptest.NewRequest(http.MethodGet, "/api/filesystem/validate?path="+url.QueryEscape(tc.path), nil))
		var got validateResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: decode %q: %v", name, rec.Body.String(), err)
		}
		if got.Kind != tc.want {
			t.Errorf("%s: kind = %q, want %q", name, got.Kind, tc.want)
		}
	}
}
