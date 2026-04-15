package session

import (
	"strings"
	"testing"
)

func TestBuildPreamble_DefaultPresetsMatchesLegacy(t *testing.T) {
	// DefaultPresets (suggestParallel=true, autoCommit=true) with a worktree
	// should produce the same output as the old hardcoded preamble.
	projects := []ProjectInfo{{Name: "MyProject", Slug: "my-project"}}
	got := buildPreamble("test-session-id", "session-abc123", projects, DefaultPresets(), nil, nil, "", false)

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
	got := buildPreamble("sess-id", "main", projects, DefaultPresets(), nil, nil, "", false)

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
	got := buildPreamble("sess-id", "branch", projects, presets, nil, nil, "", false)

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
	got := buildPreamble("sess-id", "session-xyz", []ProjectInfo{{Name: "P", Slug: "p"}}, presets, nil, nil, "", false)

	if strings.Contains(got, "Commit after each milestone") {
		t.Error("commit instructions should be excluded when autoCommit is off")
	}
}

func TestBuildPreamble_AutoCommitNoWorktree(t *testing.T) {
	presets := BehaviorPresets{SuggestParallel: true, AutoCommit: true}
	got := buildPreamble("sess-id", "", []ProjectInfo{{Name: "P", Slug: "p"}}, presets, nil, nil, "", false)

	if strings.Contains(got, "Commit after each milestone") {
		t.Error("commit instructions should be excluded when no worktree branch")
	}
}

func TestBuildPreamble_PlanFirst(t *testing.T) {
	presets := BehaviorPresets{PlanFirst: true}
	got := buildPreamble("sess-id", "", nil, presets, nil, nil, "", false)

	if !strings.Contains(got, "Plan before implementing") {
		t.Error("plan-first snippet missing")
	}
}

func TestBuildPreamble_Terse(t *testing.T) {
	presets := BehaviorPresets{Terse: true}
	got := buildPreamble("sess-id", "", nil, presets, nil, nil, "", false)

	if !strings.Contains(got, "Terse mode") {
		t.Error("terse snippet missing")
	}
}

func TestBuildPreamble_CustomInstructions(t *testing.T) {
	presets := BehaviorPresets{CustomInstructions: "Only touch backend files."}
	got := buildPreamble("sess-id", "", nil, presets, nil, nil, "", false)

	if !strings.Contains(got, "Only touch backend files.") {
		t.Error("custom instructions not appended")
	}
}

func TestBuildPreamble_AllOff(t *testing.T) {
	presets := BehaviorPresets{}
	got := buildPreamble("sess-id", "branch", []ProjectInfo{{Name: "P", Slug: "p"}}, presets, nil, nil, "", false)

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

func TestBuildPreamble_ChannelContext(t *testing.T) {
	presets := BehaviorPresets{}
	ch := &ChannelPreambleInfo{
		ChannelName: "alpha-squad",
		Members: []ChannelPreambleMember{
			{Name: "backend-agent", Role: "API work", WorktreePath: "/tmp/wt1"},
			{Name: "frontend-agent", Role: "", WorktreePath: "/tmp/wt2"},
		},
	}
	got := buildPreamble("sess-id", "", nil, presets, []*ChannelPreambleInfo{ch}, nil, "", false)

	if !strings.Contains(got, "Channel Coordination") {
		t.Error("missing channel section header")
	}
	if !strings.Contains(got, `"alpha-squad"`) {
		t.Error("missing channel name")
	}
	if !strings.Contains(got, `"backend-agent"`) {
		t.Error("missing backend member")
	}
	if !strings.Contains(got, "role: API work") {
		t.Error("missing role for backend member")
	}
	if !strings.Contains(got, "/tmp/wt1") {
		t.Error("missing worktree path")
	}
	if !strings.Contains(got, "SendMessage") {
		t.Error("missing SendMessage instructions")
	}
}

func TestBuildPreamble_GlobalExtra(t *testing.T) {
	got := buildPreamble("sess-id", "", nil, BehaviorPresets{}, nil, nil, "Do not touch the production database.", false)

	if !strings.Contains(got, "Do not touch the production database.") {
		t.Error("global extra not appended")
	}
}

func TestBuildPreamble_GlobalExtraEmpty(t *testing.T) {
	with := buildPreamble("sess-id", "", nil, BehaviorPresets{}, nil, nil, "", false)
	without := buildPreamble("sess-id", "", nil, BehaviorPresets{}, nil, nil, "", false)
	if with != without {
		t.Error("empty global extra should produce identical output")
	}
}

func TestBuildPreamble_DelegationAlwaysPresent(t *testing.T) {
	// Delegation should be present regardless of preset configuration.
	tests := []struct {
		name    string
		presets BehaviorPresets
	}{
		{"all off", BehaviorPresets{}},
		{"suggest parallel off", BehaviorPresets{AutoCommit: true}},
		{"suggest parallel on", BehaviorPresets{SuggestParallel: true}},
		{"defaults", DefaultPresets()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPreamble("sess-id", "", nil, tt.presets, nil, nil, "", false)
			if !strings.Contains(got, "## Delegation") {
				t.Error("delegation section must always be present")
			}
			if !strings.Contains(got, "@spawn") {
				t.Error("@spawn instructions must always be present")
			}
		})
	}
}

func TestBuildPreamble_TeamIdentity(t *testing.T) {
	presets := BehaviorPresets{}
	team := &TeamPreambleInfo{
		TeamName:    "Core Team",
		ProfileName: "Backend Expert",
		ProfileRole: "backend architect",
		Teammates: []TeamPreambleMember{
			{Name: "Frontend Lead", Role: "frontend expert"},
			{Name: "DevOps", Role: ""},
		},
	}
	got := buildPreamble("sess-id", "", nil, presets, nil, []*TeamPreambleInfo{team}, "", false)

	if !strings.Contains(got, "Team Identity") {
		t.Error("missing team section header")
	}
	if !strings.Contains(got, `"Backend Expert"`) {
		t.Error("missing profile name")
	}
	if !strings.Contains(got, "backend architect") {
		t.Error("missing profile role")
	}
	if !strings.Contains(got, `"Core Team"`) {
		t.Error("missing team name")
	}
	if !strings.Contains(got, `"Frontend Lead"`) {
		t.Error("missing teammate")
	}
	if !strings.Contains(got, "role: frontend expert") {
		t.Error("missing teammate role")
	}
	if !strings.Contains(got, `"DevOps"`) {
		t.Error("missing DevOps teammate")
	}
}

func TestBuildPreamble_SessionFiles(t *testing.T) {
	got := buildPreamble("abc-123-def", "", nil, BehaviorPresets{}, nil, nil, "", false)

	if !strings.Contains(got, "## Session Files") {
		t.Error("missing session files section")
	}
	if !strings.Contains(got, "abc-123-def") {
		t.Error("missing session ID in files section")
	}
	if !strings.Contains(got, "/api/sessions/abc-123-def/files/") {
		t.Error("missing API URL pattern")
	}
	if !strings.Contains(got, "session-files") {
		t.Error("missing files directory path")
	}
}

func TestBuildPreamble_SessionFilesEmptyID(t *testing.T) {
	got := buildPreamble("", "", nil, BehaviorPresets{}, nil, nil, "", false)

	if strings.Contains(got, "## Session Files") {
		t.Error("session files section should be excluded when session ID is empty")
	}
}

func TestBuildWorkerPrompt(t *testing.T) {
	got := buildWorkerPrompt("alpha-squad", "backend expert", "lead-session", []string{"Frontend UI"}, "Implement the API endpoints.")

	if !strings.Contains(got, "backend expert") {
		t.Error("missing role")
	}
	if !strings.Contains(got, `"alpha-squad"`) {
		t.Error("missing channel name")
	}
	if !strings.Contains(got, `"lead-session"`) {
		t.Error("missing lead name")
	}
	if !strings.Contains(got, "Frontend UI") {
		t.Error("missing peer name")
	}
	if !strings.Contains(got, "Implement the API endpoints.") {
		t.Error("missing raw prompt")
	}
	if !strings.Contains(got, `type: "plan"`) {
		t.Error("missing plan type instruction")
	}
	if !strings.Contains(got, `type: "progress"`) {
		t.Error("missing progress type instruction")
	}
	if !strings.Contains(got, `type: "done"`) {
		t.Error("missing done type instruction")
	}
	if !strings.Contains(got, `to: "lead-session"`) {
		t.Error("SendMessage examples should include lead name")
	}
}

func TestBuildWorkerPrompt_NoPeers(t *testing.T) {
	got := buildWorkerPrompt("team", "expert", "lead", nil, "Do stuff.")
	if strings.Contains(got, "teammates") {
		t.Error("should not mention teammates when there are none")
	}
}

func TestBuildWorkerPrompt_EmptyRole(t *testing.T) {
	got := buildWorkerPrompt("team", "", "lead", nil, "Do stuff.")
	if !strings.Contains(got, "worker") {
		t.Error("empty role should default to 'worker'")
	}
}
