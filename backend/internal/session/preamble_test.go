package session

import (
	"strings"
	"testing"
)

func TestBuildPreamble_DefaultPresetsMatchesLegacy(t *testing.T) {
	// DefaultPresets (suggestParallel=true, autoCommit=true) with a worktree
	// should produce the same output as the old hardcoded preamble.
	projects := []ProjectInfo{{Name: "MyProject", Slug: "my-project"}}
	got := buildPreamble("session-abc123", projects, DefaultPresets())

	// Must contain identity
	if !strings.Contains(got, "You are running inside Agentique") {
		t.Error("missing identity line")
	}
	// Must contain parallel session suggestion
	if !strings.Contains(got, "suggest session prompts") {
		t.Error("missing parallel session suggestion")
	}
	// Must contain commit instructions with branch name
	if !strings.Contains(got, `"session-abc123"`) {
		t.Error("missing worktree commit instructions with branch name")
	}
	if !strings.Contains(got, "Commit after each milestone") {
		t.Error("missing commit milestone instruction")
	}
	// Single project: no cross-project instructions
	if strings.Contains(got, "Available projects:") {
		t.Error("should not include cross-project instructions for single project")
	}
}

func TestBuildPreamble_DefaultPresetsMultiProject(t *testing.T) {
	projects := []ProjectInfo{
		{Name: "Frontend", Slug: "frontend"},
		{Name: "Backend", Slug: "backend"},
	}
	got := buildPreamble("main", projects, DefaultPresets())

	if !strings.Contains(got, "Available projects:") {
		t.Error("missing cross-project instructions")
	}
	if !strings.Contains(got, "`frontend`") {
		t.Error("missing frontend project in list")
	}
	if !strings.Contains(got, "`backend`") {
		t.Error("missing backend project in list")
	}
}

func TestBuildPreamble_SuggestParallelOff(t *testing.T) {
	presets := BehaviorPresets{SuggestParallel: false, AutoCommit: true}
	projects := []ProjectInfo{
		{Name: "A", Slug: "a"},
		{Name: "B", Slug: "b"},
	}
	got := buildPreamble("branch", projects, presets)

	if strings.Contains(got, "suggest session prompts") {
		t.Error("parallel suggestion text should be excluded")
	}
	// Cross-project instructions depend on SuggestParallel
	if strings.Contains(got, "Available projects:") {
		t.Error("cross-project instructions should be excluded when SuggestParallel is off")
	}
	// Identity still present
	if !strings.Contains(got, "You are running inside Agentique") {
		t.Error("identity line must always be present")
	}
}

func TestBuildPreamble_AutoCommitOff(t *testing.T) {
	presets := BehaviorPresets{SuggestParallel: true, AutoCommit: false}
	got := buildPreamble("session-xyz", []ProjectInfo{{Name: "P", Slug: "p"}}, presets)

	if strings.Contains(got, "Commit after each milestone") {
		t.Error("commit instructions should be excluded when autoCommit is off")
	}
}

func TestBuildPreamble_AutoCommitNoWorktree(t *testing.T) {
	presets := BehaviorPresets{SuggestParallel: true, AutoCommit: true}
	got := buildPreamble("", []ProjectInfo{{Name: "P", Slug: "p"}}, presets)

	if strings.Contains(got, "Commit after each milestone") {
		t.Error("commit instructions should be excluded when no worktree branch")
	}
}

func TestBuildPreamble_PlanFirst(t *testing.T) {
	presets := BehaviorPresets{PlanFirst: true}
	got := buildPreamble("", nil, presets)

	if !strings.Contains(got, "Plan before implementing") {
		t.Error("plan-first snippet missing")
	}
}

func TestBuildPreamble_Terse(t *testing.T) {
	presets := BehaviorPresets{Terse: true}
	got := buildPreamble("", nil, presets)

	if !strings.Contains(got, "Terse mode") {
		t.Error("terse snippet missing")
	}
}

func TestBuildPreamble_CustomInstructions(t *testing.T) {
	presets := BehaviorPresets{CustomInstructions: "Only touch backend files."}
	got := buildPreamble("", nil, presets)

	if !strings.Contains(got, "Only touch backend files.") {
		t.Error("custom instructions not appended")
	}
}

func TestBuildPreamble_AllOff(t *testing.T) {
	presets := BehaviorPresets{}
	got := buildPreamble("branch", []ProjectInfo{{Name: "P", Slug: "p"}}, presets)

	// Should only contain identity
	if !strings.Contains(got, "You are running inside Agentique") {
		t.Error("identity line must always be present")
	}
	if strings.Contains(got, "suggest session prompts") {
		t.Error("parallel suggestion should be off")
	}
	if strings.Contains(got, "Commit after each milestone") {
		t.Error("commit instructions should be off")
	}
	if strings.Contains(got, "Plan before implementing") {
		t.Error("plan-first should be off")
	}
	if strings.Contains(got, "Terse mode") {
		t.Error("terse should be off")
	}
}
