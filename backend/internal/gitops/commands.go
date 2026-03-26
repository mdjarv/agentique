package gitops

import (
	"os"
	"path/filepath"
	"strings"
)

// extractDescription reads YAML frontmatter from a markdown file and returns
// the description value, or "" if not found.
func extractDescription(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	content := string(buf[:n])

	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}
	frontmatter := content[4 : 4+end]

	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "description:") {
			continue
		}
		val := strings.TrimSpace(line[len("description:"):])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		return val
	}
	return ""
}

// CommandFile represents a custom slash command from .claude/commands/.
type CommandFile struct {
	Name        string `json:"name"`        // filename without .md
	Source      string `json:"source"`      // "project" or "user"
	Description string `json:"description"` // from YAML frontmatter
}

// ListCommandFiles scans project-local and user-global .claude/commands/ dirs
// for .md files. Project commands shadow user commands on name collision.
func ListCommandFiles(projectDir string) ([]CommandFile, error) {
	seen := make(map[string]struct{})
	var result []CommandFile

	// Project-local commands (higher priority).
	projectCmds, _ := filepath.Glob(filepath.Join(projectDir, ".claude", "commands", "*.md"))
	for _, p := range projectCmds {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		seen[name] = struct{}{}
		result = append(result, CommandFile{Name: name, Source: "project", Description: extractDescription(p)})
	}

	// User-global commands.
	home, err := os.UserHomeDir()
	if err != nil {
		return result, nil
	}
	userCmds, _ := filepath.Glob(filepath.Join(home, ".claude", "commands", "*.md"))
	for _, p := range userCmds {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if _, exists := seen[name]; exists {
			continue
		}
		result = append(result, CommandFile{Name: name, Source: "user", Description: extractDescription(p)})
	}

	return result, nil
}
