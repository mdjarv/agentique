package server

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func newCatalogTestDB(t *testing.T) *store.Queries {
	t.Helper()
	// Point the CLI-extras layer at an empty dir so the developer's own
	// ~/.claude.json can't leak extra models into these assertions.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func claudeModels(t *testing.T, q *store.Queries, overrides map[string][]config.ModelOverride) []struct {
	Slug  string
	Label string
} {
	t.Helper()
	res := modelCatalog(q, overrides).ListModels(context.Background())
	for _, p := range res.Providers {
		if p.Provider != "claude" {
			continue
		}
		out := make([]struct {
			Slug  string
			Label string
		}, 0, len(p.Models))
		for _, m := range p.Models {
			out = append(out, struct {
				Slug  string
				Label string
			}{m.Slug, m.DisplayName})
		}
		return out
	}
	t.Fatal("no claude provider")
	return nil
}

func labelOf(models []struct {
	Slug  string
	Label string
}, slug string) string {
	for _, m := range models {
		if m.Slug == slug {
			return m.Label
		}
	}
	return ""
}

// A session's init event records the concrete model without changing the
// stable family label in the global picker.
func TestModelCatalogKeepsStableLabels(t *testing.T) {
	q := newCatalogTestDB(t)

	if got := labelOf(claudeModels(t, q, nil), "opus[1m]"); got != "Opus" {
		t.Fatalf("pre-learn opus label = %q, want Opus", got)
	}

	// What EventPipeline.handleInit → persistResolvedModel writes when a session
	// started on "opus[1m]" reports claude-opus-5[1m].
	if err := q.UpsertModelResolution(context.Background(), store.UpsertModelResolutionParams{
		Provider:   "claude",
		Slug:       "opus[1m]",
		ResolvedID: "claude-opus-5[1m]",
	}); err != nil {
		t.Fatal(err)
	}

	if got := labelOf(claudeModels(t, q, nil), "opus[1m]"); got != "Opus" {
		t.Errorf("post-learn opus label = %q, want Opus", got)
	}

	// A later release moves the alias again; the upsert re-points it in place.
	if err := q.UpsertModelResolution(context.Background(), store.UpsertModelResolutionParams{
		Provider:   "claude",
		Slug:       "opus[1m]",
		ResolvedID: "claude-opus-5-1[1m]",
	}); err != nil {
		t.Fatal(err)
	}
	if got := labelOf(claudeModels(t, q, nil), "opus[1m]"); got != "Opus" {
		t.Errorf("relearned opus label = %q, want Opus", got)
	}
}

func TestModelCatalogConfigOverrideWins(t *testing.T) {
	q := newCatalogTestDB(t)
	if err := q.UpsertModelResolution(context.Background(), store.UpsertModelResolutionParams{
		Provider: "claude", Slug: "opus", ResolvedID: "claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}

	models := claudeModels(t, q, map[string][]config.ModelOverride{
		"claude": {{Slug: "opus", Display: "House Opus"}},
	})
	if len(models) != 1 || models[0].Label != "House Opus" {
		t.Errorf("override ignored: %+v", models)
	}
}

func TestModelCatalogSurvivesClosedDB(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)
	closeDB(t, db)

	// A catalog request must still answer when the resolutions read fails.
	if got := labelOf(claudeModels(t, q, nil), "opus[1m]"); got != "Opus" {
		t.Errorf("opus label = %q, want Opus", got)
	}
}

func closeDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
