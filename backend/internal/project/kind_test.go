package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

func TestKindOf(t *testing.T) {
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
		want Kind
	}{
		"repository root":            {repo, KindGit},
		"folder inside a repository": {sub, KindFolder},
		"plain folder":               {t.TempDir(), KindFolder},
		"missing folder":             {filepath.Join(repo, "gone"), KindFolder},
	}
	for name, tc := range cases {
		if got := KindOf(tc.path); got != tc.want {
			t.Errorf("%s: KindOf = %q, want %q", name, got, tc.want)
		}
	}
}

// The wire form is the stored row, flattened, plus kind — the shape the
// generated client schema expects.
func TestToWireMarshal(t *testing.T) {
	raw, err := json.Marshal(ToWire(store.Project{ID: "p1", Name: "home", Path: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{`"id":"p1"`, `"name":"home"`, `"kind":"folder"`} {
		if !strings.Contains(got, want) {
			t.Errorf("wire JSON %s lacks %s", got, want)
		}
	}
}
