package session

import (
	"fmt"
	"strings"

	"github.com/allbin/agentique/backend/internal/store"
)

const preambleBase = `You are running inside Agentique, a GUI that manages parallel Claude Code sessions across projects. Each session runs in its own git worktree for isolation.

When you identify independent tasks that could be worked on in parallel, suggest session prompts using fenced blocks with the "prompt" language tag. Put a markdown heading (# Title) as the first line — this becomes the session name. The user can launch these as separate sessions with one click. Example:

` + "```prompt" + `
# Refactor auth middleware
Refactor the auth middleware in backend/internal/auth to use the new token validation library. See CLAUDE.md for conventions.
` + "```" + `

Only suggest session prompts when the work is genuinely parallelizable — don't force it.`

const crossProjectInstructions = `

To target a different project, add a ` + "`project: <slug>`" + ` line immediately after the title:

` + "```prompt" + `
# Fix API client
project: %s
Fix the API client timeout handling.
` + "```" + `

Available projects:
%s`

const worktreeCommitInstructions = `

This session runs in an isolated git worktree on branch %q. Your changes are fully isolated from the main branch.

**Commit after each milestone.** Override the default "only commit when asked" behavior — in this worktree, commit proactively after each logical unit of work (feature added, bug fixed, tests passing, refactor complete). Use short, descriptive commit messages. Prefer ` + "`git add <specific files>`" + ` over ` + "`git add -A`" + `. Do not ask for permission to commit — just commit when you reach a working state.`

// ProjectInfo holds the minimal project metadata needed for the preamble.
type ProjectInfo struct {
	Name string
	Slug string
}

// ProjectInfoFromStore converts store projects to the minimal preamble info.
func ProjectInfoFromStore(projects []store.Project) []ProjectInfo {
	out := make([]ProjectInfo, len(projects))
	for i, p := range projects {
		out[i] = ProjectInfo{Name: p.Name, Slug: p.Slug}
	}
	return out
}

// buildPreamble returns the system prompt preamble for a session.
// When multiple projects exist, it documents the cross-project prompt syntax.
// For worktree sessions (worktreeBranch != ""), it appends commit instructions.
func buildPreamble(worktreeBranch string, projects []ProjectInfo) string {
	s := preambleBase

	if len(projects) > 1 {
		var lines []string
		var exampleSlug string
		for _, p := range projects {
			lines = append(lines, fmt.Sprintf("- `%s` — %s", p.Slug, p.Name))
			if exampleSlug == "" {
				exampleSlug = p.Slug
			}
		}
		s += fmt.Sprintf(crossProjectInstructions, exampleSlug, strings.Join(lines, "\n"))
	}

	if worktreeBranch != "" {
		s += fmt.Sprintf(worktreeCommitInstructions, worktreeBranch)
	}
	return s
}
