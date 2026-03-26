package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"normal", "---\ndescription: Do the thing\n---\nbody", "Do the thing"},
		{"quoted double", "---\ndescription: \"Quoted desc\"\n---\n", "Quoted desc"},
		{"quoted single", "---\ndescription: 'Single quoted'\n---\n", "Single quoted"},
		{"extra keys", "---\nname: foo\ndescription: Bar baz\nallowed-tools: [Read]\n---\n", "Bar baz"},
		{"no frontmatter", "Just a markdown file", ""},
		{"no closing delimiter", "---\ndescription: Oops\n", ""},
		{"no description key", "---\nname: foo\n---\n", ""},
		{"empty file", "", ""},
		{"crlf line endings", "---\r\ndescription: Windows style\r\n---\r\n", "Windows style"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "cmd.md")
			if err := os.WriteFile(p, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := extractDescription(p)
			if got != tt.want {
				t.Errorf("extractDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListCommandFiles_descriptions(t *testing.T) {
	dir := t.TempDir()
	cmdsDir := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(cmdsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Isolate from real user-global commands.
	t.Setenv("HOME", t.TempDir())

	writeFile(t, cmdsDir, "review.md", "---\ndescription: Review code changes\n---\nBody")
	writeFile(t, cmdsDir, "deploy.md", "No frontmatter here")

	cmds, err := ListCommandFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2", len(cmds))
	}

	byName := make(map[string]CommandFile)
	for _, c := range cmds {
		byName[c.Name] = c
	}

	if d := byName["review"].Description; d != "Review code changes" {
		t.Errorf("review description = %q, want %q", d, "Review code changes")
	}
	if d := byName["deploy"].Description; d != "" {
		t.Errorf("deploy description = %q, want empty", d)
	}
}

func TestListCommandFiles_skills(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Project command.
	projCmds := filepath.Join(dir, ".claude", "commands")
	os.MkdirAll(projCmds, 0755)
	writeFile(t, projCmds, "review.md", "---\ndescription: Review code\n---\n")

	// Project skill.
	projSkill := filepath.Join(dir, ".claude", "skills", "tdd")
	os.MkdirAll(projSkill, 0755)
	writeFile(t, projSkill, "SKILL.md", "---\ndescription: Test-driven dev\n---\n")

	// User command.
	userCmds := filepath.Join(home, ".claude", "commands")
	os.MkdirAll(userCmds, 0755)
	writeFile(t, userCmds, "deploy.md", "---\ndescription: Deploy\n---\n")

	// User skill.
	userSkill := filepath.Join(home, ".claude", "skills", "got")
	os.MkdirAll(userSkill, 0755)
	writeFile(t, userSkill, "SKILL.md", "---\ndescription: Run tests\n---\n")

	// Skill dir without SKILL.md — should be skipped.
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "empty"), 0755)

	// Skill with disable-model-invocation: true — should be skipped.
	disabledSkill := filepath.Join(home, ".claude", "skills", "internal-only")
	os.MkdirAll(disabledSkill, 0755)
	writeFile(t, disabledSkill, "SKILL.md", "---\ndescription: Not a slash cmd\ndisable-model-invocation: true\n---\n")

	cmds, err := ListCommandFiles(dir)
	if err != nil {
		t.Fatal(err)
	}

	byName := make(map[string]CommandFile)
	for _, c := range cmds {
		byName[c.Name] = c
	}

	if len(byName) != 4 {
		t.Fatalf("got %d entries, want 4: %v", len(byName), byName)
	}
	if c := byName["review"]; c.Source != "project" || c.Description != "Review code" {
		t.Errorf("review = %+v", c)
	}
	if c := byName["tdd"]; c.Source != "project" || c.Description != "Test-driven dev" {
		t.Errorf("tdd = %+v", c)
	}
	if c := byName["deploy"]; c.Source != "user" || c.Description != "Deploy" {
		t.Errorf("deploy = %+v", c)
	}
	if c := byName["got"]; c.Source != "user" || c.Description != "Run tests" {
		t.Errorf("got = %+v", c)
	}
}

func TestListCommandFiles_commandShadowsSkill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// Command and skill with same name — command wins.
	cmdsDir := filepath.Join(dir, ".claude", "commands")
	os.MkdirAll(cmdsDir, 0755)
	writeFile(t, cmdsDir, "review.md", "---\ndescription: from command\n---\n")

	skillDir := filepath.Join(dir, ".claude", "skills", "review")
	os.MkdirAll(skillDir, 0755)
	writeFile(t, skillDir, "SKILL.md", "---\ndescription: from skill\n---\n")

	cmds, err := ListCommandFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d entries, want 1 (command should shadow skill)", len(cmds))
	}
	if cmds[0].Description != "from command" {
		t.Errorf("expected command description, got %q", cmds[0].Description)
	}
}
