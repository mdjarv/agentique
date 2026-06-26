package session

import "testing"

func TestClassifyTool(t *testing.T) {
	cases := []struct {
		name string
		tool string
		want string
	}{
		{"bash is command", "Bash", "command"},
		{"edit is file_write", "Edit", "file_write"},
		{"read is file_read", "Read", "file_read"},
		// The task-list family (TodoWrite's successors) groups under "task".
		{"todowrite is task", "TodoWrite", "task"},
		{"taskcreate is task", "TaskCreate", "task"},
		{"taskupdate is task", "TaskUpdate", "task"},
		{"tasklist is task", "TaskList", "task"},
		{"taskget is task", "TaskGet", "task"},
		// Background-process control is a different "task" and stays default.
		{"taskoutput is not task", "TaskOutput", "other"},
		{"taskstop is not task", "TaskStop", "other"},
		{"mcp prefix is mcp", "mcp__server__do", "mcp"},
		{"unknown is other", "Frobnicate", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTool(tc.tool); got != tc.want {
				t.Errorf("classifyTool(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}
