package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

func TestEnsureDefaultProject(t *testing.T) {
	t.Run("unset registers nothing, not even a git cwd", func(t *testing.T) {
		_, q := testutil.SetupDB(t)
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(repo)

		ensureDefaultProject(q, "")

		assertProjectPaths(t, q)
	})

	t.Run("configured directory is registered", func(t *testing.T) {
		_, q := testutil.SetupDB(t)
		dir := t.TempDir()

		ensureDefaultProject(q, dir)

		assertProjectPaths(t, q, dir)
	})

	t.Run("missing directory registers nothing", func(t *testing.T) {
		_, q := testutil.SetupDB(t)

		ensureDefaultProject(q, filepath.Join(t.TempDir(), "gone"))

		assertProjectPaths(t, q)
	})

	t.Run("ignored once any project exists", func(t *testing.T) {
		_, q := testutil.SetupDB(t)
		testutil.SeedProject(t, q, "existing", "/tmp/existing")

		ensureDefaultProject(q, t.TempDir())

		assertProjectPaths(t, q, "/tmp/existing")
	})
}

func assertProjectPaths(t *testing.T, q *store.Queries, want ...string) {
	t.Helper()
	projects, err := q.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	got := make([]string, 0, len(projects))
	for _, p := range projects {
		got = append(got, p.Path)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("project paths = %q, want %q", got, want)
	}
}
