package session

import "testing"

func TestIsPlanSafeTool(t *testing.T) {
	safe := []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch", "Agent", "ExitWorktree", "TodoWrite", "TodoRead"}
	for _, tool := range safe {
		if !isPlanSafeTool(tool) {
			t.Errorf("isPlanSafeTool(%q) = false, want true", tool)
		}
	}

	unsafe := []string{
		"Bash", "Edit", "Write", "NotebookEdit", "MultiEdit",
		"EnterPlanMode", "ExitPlanMode",
		"ToolSearch", "Skill", "AskUserQuestion",
		"mcp__foo__bar", "LSP", "UnknownTool",
	}
	for _, tool := range unsafe {
		if isPlanSafeTool(tool) {
			t.Errorf("isPlanSafeTool(%q) = true, want false", tool)
		}
	}
}

func TestIsAutoSafeTool(t *testing.T) {
	safe := []string{
		"Read", "Glob", "Grep", // file_read
		"Edit", "Write", "NotebookEdit", "MultiEdit", // file_write
		"WebSearch", "WebFetch", // web
		"Agent", "ExitWorktree", // agent
		"TodoWrite", "TodoRead", // task
		"ToolSearch", "Skill", // meta
		"AskUserQuestion", // question
	}
	for _, tool := range safe {
		if !isAutoSafeTool(tool) {
			t.Errorf("isAutoSafeTool(%q) = false, want true", tool)
		}
	}

	unsafe := []string{
		"Bash",
		"EnterPlanMode", "ExitPlanMode",
		"mcp__foo__bar", "LSP", "UnknownTool",
	}
	for _, tool := range unsafe {
		if isAutoSafeTool(tool) {
			t.Errorf("isAutoSafeTool(%q) = true, want false", tool)
		}
	}
}

func TestShouldBypassPermission(t *testing.T) {
	tests := []struct {
		name string
		auto string
		perm string
		tool string
		want bool
	}{
		// EnterPlanMode always bypassed.
		{"EnterPlanMode-manual", "manual", "default", "EnterPlanMode", true},
		{"EnterPlanMode-auto", "auto", "default", "EnterPlanMode", true},
		{"EnterPlanMode-fullAuto", "fullAuto", "default", "EnterPlanMode", true},
		{"EnterPlanMode-auto-plan", "auto", "plan", "EnterPlanMode", true},

		// fullAuto always bypasses.
		{"fullAuto-Bash", "fullAuto", "default", "Bash", true},
		{"fullAuto-Edit", "fullAuto", "default", "Edit", true},
		{"fullAuto-Read", "fullAuto", "default", "Read", true},
		{"fullAuto-mcp", "fullAuto", "default", "mcp__foo__bar", true},
		{"fullAuto-ExitPlanMode", "fullAuto", "plan", "ExitPlanMode", true},

		// manual never bypasses.
		{"manual-Read", "manual", "default", "Read", false},
		{"manual-Bash", "manual", "default", "Bash", false},
		{"manual-ToolSearch", "manual", "default", "ToolSearch", false},

		// auto + default: auto-safe bypass, others blocked.
		{"auto-default-Read", "auto", "default", "Read", true},
		{"auto-default-Grep", "auto", "default", "Grep", true},
		{"auto-default-ToolSearch", "auto", "default", "ToolSearch", true},
		{"auto-default-AskUserQuestion", "auto", "default", "AskUserQuestion", true},
		{"auto-default-Agent", "auto", "default", "Agent", true},
		{"auto-default-Bash", "auto", "default", "Bash", false},
		{"auto-default-Edit", "auto", "default", "Edit", true},
		{"auto-default-Write", "auto", "default", "Write", true},
		{"auto-default-mcp", "auto", "default", "mcp__server__tool", false},
		{"auto-default-ExitPlanMode", "auto", "default", "ExitPlanMode", false},

		// auto + plan: plan-safe bypass, others blocked.
		{"auto-plan-Read", "auto", "plan", "Read", true},
		{"auto-plan-Grep", "auto", "plan", "Grep", true},
		{"auto-plan-Agent", "auto", "plan", "Agent", true},
		{"auto-plan-ToolSearch", "auto", "plan", "ToolSearch", false},
		{"auto-plan-AskUserQuestion", "auto", "plan", "AskUserQuestion", false},
		{"auto-plan-Bash", "auto", "plan", "Bash", false},
		{"auto-plan-Edit", "auto", "plan", "Edit", false},
		{"auto-plan-ExitPlanMode", "auto", "plan", "ExitPlanMode", false},

		// auto + acceptEdits: same as default.
		{"auto-acceptEdits-Read", "auto", "acceptEdits", "Read", true},
		{"auto-acceptEdits-Bash", "auto", "acceptEdits", "Bash", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldBypassPermission(tt.auto, tt.perm, tt.tool)
			if got != tt.want {
				t.Errorf("shouldBypassPermission(%q, %q, %q) = %v, want %v",
					tt.auto, tt.perm, tt.tool, got, tt.want)
			}
		})
	}
}
