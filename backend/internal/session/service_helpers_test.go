package session

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// resolveSessionConfig is the explicit > persona > project default cascade.
// These tests pin down each branch of the cascade so future contributors
// can't accidentally break the precedence ordering.

func TestResolveSessionConfig_ExplicitWinsOverPersona(t *testing.T) {
	p := CreateSessionParams{
		Model:           "sonnet",
		Effort:          "high",
		AutoApproveMode: "auto",
		BehaviorPresets: BehaviorPresets{AutoCommit: true},
	}
	pc := PersonaConfig{
		Model:           "haiku",
		Effort:          "low",
		AutoApproveMode: "manual",
		BehaviorPresets: BehaviorPresets{Terse: true},
	}
	got := resolveSessionConfig(p, pc, store.Project{})

	if got.model != "sonnet" {
		t.Errorf("model: got %q, want sonnet (explicit wins)", got.model)
	}
	if got.effort != "high" {
		t.Errorf("effort: got %q, want high", got.effort)
	}
	if got.autoApproveMode != "auto" {
		t.Errorf("autoApproveMode: got %q, want auto", got.autoApproveMode)
	}
	if !got.presets.AutoCommit || got.presets.Terse {
		t.Errorf("presets: got %+v, want explicit (AutoCommit only)", got.presets)
	}
}

func TestResolveSessionConfig_PersonaFillsBlanks(t *testing.T) {
	p := CreateSessionParams{}
	pc := PersonaConfig{
		Model:           "haiku",
		Effort:          "medium",
		AutoApproveMode: "manual",
		BehaviorPresets: BehaviorPresets{PlanFirst: true, CustomInstructions: "x"},
	}
	got := resolveSessionConfig(p, pc, store.Project{})

	if got.model != "haiku" {
		t.Errorf("model: got %q, want haiku (persona fallback)", got.model)
	}
	if got.effort != "medium" {
		t.Errorf("effort: got %q, want medium", got.effort)
	}
	if got.autoApproveMode != "manual" {
		t.Errorf("autoApproveMode: got %q, want manual", got.autoApproveMode)
	}
	if !got.presets.PlanFirst || got.presets.CustomInstructions != "x" {
		t.Errorf("presets: persona presets should be copied, got %+v", got.presets)
	}
}

func TestResolveSessionConfig_ProjectDefaultsForPresets(t *testing.T) {
	p := CreateSessionParams{}
	pc := PersonaConfig{} // empty
	project := store.Project{
		DefaultBehaviorPresets: `{"autoCommit":true,"terse":true}`,
	}
	got := resolveSessionConfig(p, pc, project)

	if !got.presets.AutoCommit || !got.presets.Terse {
		t.Errorf("presets should come from project default, got %+v", got.presets)
	}
}

func TestResolveSessionConfig_OpusFallbackWhenNothingSet(t *testing.T) {
	got := resolveSessionConfig(CreateSessionParams{}, PersonaConfig{}, store.Project{})
	if got.model != "opus" {
		t.Errorf("model fallback: got %q, want opus", got.model)
	}
}

func TestResolveSessionConfig_PartialExplicitMixesWithPersona(t *testing.T) {
	// Caller specifies model only — effort/autoApprove must come from persona.
	p := CreateSessionParams{Model: "sonnet"}
	pc := PersonaConfig{Effort: "low", AutoApproveMode: "manual"}
	got := resolveSessionConfig(p, pc, store.Project{})

	if got.model != "sonnet" {
		t.Errorf("model: got %q, want sonnet (explicit)", got.model)
	}
	if got.effort != "low" {
		t.Errorf("effort: got %q, want low (persona)", got.effort)
	}
	if got.autoApproveMode != "manual" {
		t.Errorf("autoApproveMode: got %q, want manual (persona)", got.autoApproveMode)
	}
}

func TestResolveSessionConfig_ExplicitPresetsZeroDoesNotInheritPersona(t *testing.T) {
	// IsZero() on caller-side BehaviorPresets means "no preference". Persona
	// presets fill in. ParsePresets({}) should not fire because persona has
	// non-zero presets.
	p := CreateSessionParams{}
	pc := PersonaConfig{BehaviorPresets: BehaviorPresets{Terse: true}}
	got := resolveSessionConfig(p, pc, store.Project{DefaultBehaviorPresets: `{"autoCommit":true}`})

	// Should be persona presets, NOT project defaults.
	if !got.presets.Terse {
		t.Errorf("presets: persona Terse should win over project default, got %+v", got.presets)
	}
	if got.presets.AutoCommit {
		t.Errorf("presets: project AutoCommit should not leak when persona was set, got %+v", got.presets)
	}
}

// baseSessionInfo is a pure projection from a store.Session row. The test
// guards against a typo silently dropping a field on the wire payload.

func TestBaseSessionInfo_FullProjection(t *testing.T) {
	ss := store.Session{
		ID:              "sess-1",
		ProjectID:       "proj-1",
		Name:            "Test",
		State:           "idle",
		Model:           "opus",
		PermissionMode:  "manual",
		AutoApproveMode: "manual",
		Effort:          "high",
		MaxBudget:       42.5,
		MaxTurns:        20,
		WorktreePath:    sql.NullString{String: "/tmp/wt", Valid: true},
		WorktreeBranch:  sql.NullString{String: "feature-x", Valid: true},
		WorktreeMerged:  1,
		CompletedAt:     sql.NullString{String: "2026-04-01", Valid: true},
		PrUrl:           "https://example/pr/1",
		BehaviorPresets: `{"autoCommit":true}`,
		ParentSessionID: sql.NullString{String: "parent-1", Valid: true},
		CreatedAt:       "2026-04-01",
		UpdatedAt:       "2026-04-02",
		LastQueryAt:     sql.NullString{String: "2026-04-03", Valid: true},
	}
	got := baseSessionInfo(ss)

	if got.ID != "sess-1" || got.ProjectID != "proj-1" || got.Name != "Test" {
		t.Errorf("identity fields: %+v", got)
	}
	if got.WorktreePath != "/tmp/wt" || got.WorktreeBranch != "feature-x" {
		t.Errorf("worktree fields: %+v", got)
	}
	if !got.WorktreeMerged {
		t.Error("WorktreeMerged: int 1 should map to true")
	}
	if got.CompletedAt != "2026-04-01" {
		t.Errorf("CompletedAt: got %q", got.CompletedAt)
	}
	if got.ParentSessionID != "parent-1" {
		t.Errorf("ParentSessionID: got %q", got.ParentSessionID)
	}
	if got.LastQueryAt != "2026-04-03" {
		t.Errorf("LastQueryAt: got %q", got.LastQueryAt)
	}
	if got.MaxBudget != 42.5 || got.MaxTurns != 20 {
		t.Errorf("budget/turns: %+v", got)
	}
	if !got.BehaviorPresets.AutoCommit {
		t.Errorf("BehaviorPresets should be parsed: %+v", got.BehaviorPresets)
	}
}

func TestBaseSessionInfo_NullStringsBecomeEmpty(t *testing.T) {
	got := baseSessionInfo(store.Session{
		ID:        "x",
		ProjectID: "y",
		// All sql.NullString fields are zero-value (Valid: false).
	})
	if got.WorktreePath != "" || got.WorktreeBranch != "" || got.CompletedAt != "" ||
		got.ParentSessionID != "" || got.LastQueryAt != "" {
		t.Errorf("invalid NullStrings should be empty: %+v", got)
	}
	if got.WorktreeMerged {
		t.Error("WorktreeMerged: int 0 should map to false")
	}
}

func TestBaseSessionInfo_DefaultPresetsForEmptyJSON(t *testing.T) {
	got := baseSessionInfo(store.Session{ID: "x", BehaviorPresets: ""})
	want := DefaultPresets()
	if !reflect.DeepEqual(got.BehaviorPresets, want) {
		t.Errorf("empty preset JSON should yield DefaultPresets, got %+v", got.BehaviorPresets)
	}
}

// applyPostResumeFlags re-applies persisted lifecycle flags onto a live
// Session. Because Session has methods that lock its mutex, we can construct
// a minimal one here and verify the flags landed.

func TestApplyPostResumeFlags_MarksMergedAndCompleted(t *testing.T) {
	sess := &Session{ID: "s"}
	dbSess := store.Session{
		ID:             "s",
		WorktreeMerged: 1,
		CompletedAt:    sql.NullString{String: "2026-01-01", Valid: true},
	}
	applyPostResumeFlags(sess, dbSess)

	_, _, merged, completedAt, _ := sess.liveState()
	if !merged {
		t.Error("worktreeMerged should be true after MarkMerged")
	}
	if completedAt == "" {
		t.Error("completedAt should be set after MarkCompleted")
	}
}

func TestApplyPostResumeFlags_NoOpForFreshSession(t *testing.T) {
	sess := &Session{ID: "s"}
	applyPostResumeFlags(sess, store.Session{ID: "s"})

	_, _, merged, completedAt, _ := sess.liveState()
	if merged {
		t.Error("worktreeMerged should remain false")
	}
	if completedAt != "" {
		t.Errorf("completedAt should remain empty, got %q", completedAt)
	}
}

// worktreeDirExists is a thin wrapper over os.Stat. Smoke-test against tempdir.

func TestWorktreeDirExists(t *testing.T) {
	tmp := t.TempDir()
	if !worktreeDirExists(tmp) {
		t.Errorf("expected true for existing dir %s", tmp)
	}
	if worktreeDirExists(tmp + "/does-not-exist") {
		t.Error("expected false for missing path")
	}
}
