package gitops

import (
	"os"
	"path/filepath"
	"strings"
)

// CommandFile represents a custom slash command from .claude/commands/.
type CommandFile struct {
	Name   string `json:"name"`   // filename without .md
	Source string `json:"source"` // "project" or "user"
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
		result = append(result, CommandFile{Name: name, Source: "project"})
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
		result = append(result, CommandFile{Name: name, Source: "user"})
	}

	return result, nil
}
