package session

import (
	"fmt"
	"strings"

	"github.com/allbin/agentique/backend/internal/store"
)

// preambleIdentity is always emitted — establishes Agentique context.
const preambleIdentity = `You are running inside Agentique, a GUI that manages parallel Claude Code sessions across projects. Each session runs in its own git worktree for isolation.`

// presetSuggestParallel instructs Claude to suggest parallelizable work as prompt blocks.
const presetSuggestParallel = `

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

// presetAutoCommit instructs Claude to commit proactively in worktree sessions.
const presetAutoCommit = `

This session runs in an isolated git worktree on branch %q. Your changes are fully isolated from the main branch.

**Commit after each milestone.** Override the default "only commit when asked" behavior — in this worktree, commit proactively after each logical unit of work (feature added, bug fixed, tests passing, refactor complete). Use short, descriptive commit messages. Prefer ` + "`git add <specific files>`" + ` over ` + "`git add -A`" + `. Do not ask for permission to commit — just commit when you reach a working state.`

// presetPlanFirst instructs Claude to outline a plan before implementing.
const presetPlanFirst = `

**Plan before implementing.** Before writing code, outline your approach: what you'll change, which files are involved, and any trade-offs. Wait for confirmation before proceeding. This is a soft guideline — use judgment for trivial changes.`

// presetTerse instructs Claude to minimize output.
const presetTerse = `

**Terse mode.** Be extremely concise. Skip explanations unless asked. Show code changes directly. No summaries, no preamble, no sign-offs.`

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
// Snippets are conditionally included based on the behavior presets.
func buildPreamble(worktreeBranch string, projects []ProjectInfo, presets BehaviorPresets) string {
	s := preambleIdentity

	if presets.SuggestParallel {
		s += presetSuggestParallel
	}

	if presets.SuggestParallel && len(projects) > 1 {
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

	if presets.AutoCommit && worktreeBranch != "" {
		s += fmt.Sprintf(presetAutoCommit, worktreeBranch)
	}

	if presets.PlanFirst {
		s += presetPlanFirst
	}

	if presets.Terse {
		s += presetTerse
	}

	if presets.CustomInstructions != "" {
		s += "\n\n" + presets.CustomInstructions
	}

	return s
}
