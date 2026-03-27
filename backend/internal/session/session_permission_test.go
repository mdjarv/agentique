package session

import "testing"

func TestIsPlanSafeTool(t *testing.T) {
	safe := []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch", "Agent", "TodoWrite", "TodoRead"}
	for _, tool := range safe {
		if !isPlanSafeTool(tool) {
			t.Errorf("isPlanSafeTool(%q) = false, want true", tool)
		}
	}

	unsafe := []string{
		"Bash", "Edit", "Write", "NotebookEdit", "MultiEdit",
		"EnterPlanMode", "ExitPlanMode",
		"mcp__foo__bar", "LSP", "UnknownTool",
	}
	for _, tool := range unsafe {
		if isPlanSafeTool(tool) {
			t.Errorf("isPlanSafeTool(%q) = true, want false", tool)
		}
	}
}
