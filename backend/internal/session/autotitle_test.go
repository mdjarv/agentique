package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeProjectDir(t *testing.T) {
	cases := []struct {
		name, home, workDir, want string
	}{
		{
			name:    "simple git path",
			home:    "/home/u",
			workDir: "/home/u/git/agentique",
			want:    "/home/u/.claude/projects/-home-u-git-agentique",
		},
		{
			name:    "path with dot segment",
			home:    "/home/u",
			workDir: "/home/u/.local/share/agentique/worktrees/agentique/session-abc123",
			want:    "/home/u/.claude/projects/-home-u--local-share-agentique-worktrees-agentique-session-abc123",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := claudeProjectDir(c.home, c.workDir)
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}

func TestReadUserTurns(t *testing.T) {
	home := t.TempDir()
	workDir := "/home/u/git/repo"
	claudeSessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	dir := claudeProjectDir(home, workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Mix: custom-title (ignored), attachment (ignored), user string, user array w/ text+tool_result, assistant (ignored).
	lines := []string{
		`{"type":"custom-title","customTitle":"Ignore me","sessionId":"x"}`,
		`{"type":"attachment","attachment":{"type":"hook_non_blocking_error"}}`,
		`{"type":"user","message":{"role":"user","content":"First human message"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ignored"},{"type":"text","text":"Follow-up clarification"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":"hi"}}`,
		`not-json-should-skip`,
	}
	var data []byte
	for _, l := range lines {
		data = append(data, []byte(l+"\n")...)
	}
	if err := os.WriteFile(filepath.Join(dir, claudeSessionID+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	turns, err := readUserTurns(home, workDir, claudeSessionID, 0, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d: %+v", len(turns), turns)
	}
	if turns[0] != "First human message" {
		t.Errorf("turn[0] = %q", turns[0])
	}
	if turns[1] != "Follow-up clarification" {
		t.Errorf("turn[1] = %q", turns[1])
	}
}

func TestReadUserTurnsMissingSessionID(t *testing.T) {
	if _, err := readUserTurns("/tmp", "/any", "", 0, 0); err == nil {
		t.Error("expected error on empty claudeSessionID")
	}
}

func TestReadUserTurnsMaxTurns(t *testing.T) {
	home := t.TempDir()
	workDir := "/w"
	id := "00000000-0000-0000-0000-000000000000"
	dir := claudeProjectDir(home, workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	var data []byte
	for i := 0; i < 5; i++ {
		data = append(data, []byte(`{"type":"user","message":{"role":"user","content":"msg"}}`+"\n")...)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	turns, err := readUserTurns(home, workDir, id, 3, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(turns) != 3 {
		t.Errorf("expected 3 turns (cap), got %d", len(turns))
	}
}
