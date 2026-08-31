package gitops

import (
	"os"
	"path/filepath"
	"strings"
)

// frontmatter holds parsed YAML frontmatter fields from a markdown file.
type frontmatter struct {
	description string
	// userInvocable mirrors the `user-invocable` field, which decides whether a skill
	// is a slash command at all. It defaults to TRUE, so the zero value is wrong and
	// parseFrontmatter seeds it before reading. Do not confuse it with
	// `disable-model-invocation`, which restricts the other side — see listSkills.
	userInvocable bool
}

// parseFrontmatter reads YAML frontmatter from a markdown file.
func parseFrontmatter(path string) frontmatter {
	f, err := os.Open(path)
	if err != nil {
		return frontmatter{}
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	content := string(buf[:n])

	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return frontmatter{}
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return frontmatter{}
	}
	block := content[4 : 4+end]

	fm := frontmatter{userInvocable: true}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			fm.description = unquote(strings.TrimSpace(line[len("description:"):]))
		} else if strings.HasPrefix(line, "user-invocable:") {
			fm.userInvocable = unquote(strings.TrimSpace(line[len("user-invocable:"):])) != "false"
		}
	}
	return fm
}

func unquote(val string) string {
	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		return val[1 : len(val)-1]
	}
	return val
}

// extractDescription reads YAML frontmatter from a markdown file and returns
// the description value, or "" if not found.
func extractDescription(path string) string {
	return parseFrontmatter(path).description
}

// CommandFile represents a slash command or skill.
type CommandFile struct {
	Name        string `json:"name"`        // filename without .md, or skill directory name
	Source      string `json:"source"`      // "project" or "user"
	Description string `json:"description"` // from YAML frontmatter
}

// ListCommandFiles scans project-local and user-global .claude/commands/ and
// .claude/skills/ directories. Priority: project commands > project skills >
// user commands > user skills. First-seen name wins on collision.
func ListCommandFiles(projectDir string) ([]CommandFile, error) {
	seen := make(map[string]struct{})
	var result []CommandFile

	projectBase := filepath.Join(projectDir, ".claude")

	// Project-local commands (highest priority).
	result = append(result, listCommands(filepath.Join(projectBase, "commands"), "project", seen)...)

	// Project-local skills.
	result = append(result, listSkills(filepath.Join(projectBase, "skills"), "project", seen)...)

	// User-global.
	home, err := os.UserHomeDir()
	if err != nil {
		return result, nil
	}
	userBase := filepath.Join(home, ".claude")

	// User commands.
	result = append(result, listCommands(filepath.Join(userBase, "commands"), "user", seen)...)

	// User skills.
	result = append(result, listSkills(filepath.Join(userBase, "skills"), "user", seen)...)

	return result, nil
}

func listCommands(dir, source string, seen map[string]struct{}) []CommandFile {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	var result []CommandFile
	for _, p := range matches {
		name := strings.TrimSuffix(filepath.Base(p), ".md")
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, CommandFile{Name: name, Source: source, Description: extractDescription(p)})
	}
	return result
}

func listSkills(dir, source string, seen map[string]struct{}) []CommandFile {
	entries, _ := os.ReadDir(dir)
	var result []CommandFile
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		name := e.Name()
		if _, exists := seen[name]; exists {
			continue
		}
		skillPath := filepath.Join(dir, name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}
		// `user-invocable: false` is the one field that means "not a slash command":
		// background knowledge only Claude loads, where /name is not an action anyone
		// would take. `disable-model-invocation: true` is the OTHER restriction — it
		// says only the *human* may invoke it, which makes those skills the ones most
		// certainly belonging in this list. Filtering on it hid /handoff, /implement,
		// /triage and a dozen more, i.e. exactly the deliberate side-effecting
		// workflows the field exists to protect.
		fm := parseFrontmatter(skillPath)
		if !fm.userInvocable {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, CommandFile{Name: name, Source: source, Description: fm.description})
	}
	return result
}
