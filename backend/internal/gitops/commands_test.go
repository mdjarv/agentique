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
