package session

import (
	"database/sql"
	"testing"
	"time"

	"github.com/allbin/agentkit/worktree"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func nn(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func TestJanitorSessions_MapsFieldsAndResolvesProject(t *testing.T) {
	sessions := []store.Session{
		{
			ID:             "s1",
			ProjectID:      "p1",
			State:          "stopped",
			Name:           "Finished work",
			WorktreePath:   nn("/data/worktrees/proj/session-s1"),
			WorktreeBranch: nn("session-s1"),
			UpdatedAt:      "2026-07-03T05:12:47Z",
		},
		{ID: "s2", ProjectID: "missing", State: "running"}, // no worktree, unknown project
	}
	projByID := map[string]store.Project{"p1": {ID: "p1", Path: "/repos/proj"}}

	out := JanitorSessions(sessions, projByID)
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(out))
	}

	got := out[0]
	if got.ProjectPath != "/repos/proj" {
		t.Errorf("ProjectPath = %q, want /repos/proj", got.ProjectPath)
	}
	if got.WorktreePath != "/data/worktrees/proj/session-s1" || got.Branch != "session-s1" {
		t.Errorf("worktree fields wrong: %+v", got)
	}
	if got.State != "stopped" || got.Name != "Finished work" {
		t.Errorf("state/name wrong: %+v", got)
	}
	want := time.Date(2026, 7, 3, 5, 12, 47, 0, time.UTC)
	if !got.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want)
	}

	// Unknown project resolves to empty path (falls back to plain removal).
	if out[1].ProjectPath != "" {
		t.Errorf("unknown project should yield empty ProjectPath, got %q", out[1].ProjectPath)
	}
}

func TestParseDBTime_BadValueIsZero(t *testing.T) {
	if !parseDBTime("not-a-time").IsZero() {
		t.Error("unparseable timestamp should yield the zero time")
	}
}

func TestJanitorProjects_KeyedBySanitizedName(t *testing.T) {
	projects := []store.Project{
		{ID: "p1", Name: "The Pint", Path: "/repos/the-pint"},
		{ID: "p2", Name: "alltix-api", Path: "/repos/alltix-api"},
	}
	m := JanitorProjects(projects)

	// The worktree parent-dir name is the sanitized project name; that must be
	// the key so the janitor can tie an orphan worktree back to its project.
	if got := m[worktree.SanitizeBranch("The Pint")]; got != "/repos/the-pint" {
		t.Errorf("The Pint -> %q, want /repos/the-pint", got)
	}
	if got := m["alltix-api"]; got != "/repos/alltix-api" {
		t.Errorf("alltix-api -> %q, want /repos/alltix-api", got)
	}
}
