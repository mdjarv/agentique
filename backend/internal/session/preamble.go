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

// presetDelegation instructs Claude how to spawn worker sessions.
// Always included — delegation is a core capability, not gated by presets.
const presetDelegation = `

## Delegation

You can create a team of specialist workers, each running in its own git worktree. Use this when a task naturally decomposes into independent subtasks that different experts could tackle in parallel (e.g., "backend API + frontend UI + tests", or "migrate 3 services"). Don't spawn workers for sequential work or when you can do it faster yourself.

**How to think about teams:**
1. Identify 2-5 genuinely independent subtasks.
2. Give each worker a clear expert role and a self-contained prompt that includes all necessary context (file paths, conventions, interfaces to conform to). Workers cannot see your conversation — their prompt is all they know.
3. After spawning, wait for workers to report back, then synthesize their results, resolve conflicts between worktrees, and report the outcome to the user.

To spawn workers, use SendMessage with target ` + "`@spawn`" + `:

` + "```" + `
SendMessage({to: "@spawn", message: JSON.stringify({
  teamName: "descriptive team name",
  workers: [
    {name: "Backend API", role: "backend expert", prompt: "Implement the REST endpoints for user profiles. The schema is in db/schema.sql. Follow conventions in CLAUDE.md. When done, commit your changes and message the lead with a summary of endpoints created and any interface decisions."},
    {name: "Frontend UI", role: "frontend expert", prompt: "Build the user profile page using the existing component patterns in src/components/. The API will provide GET/PUT /api/profiles/:id. When done, commit and message the lead with your component structure."}
  ]
})})
` + "```" + `

The user must approve before workers are created. Workers join your team and communicate via SendMessage. If a worker seems to be taking too long, send them a message asking for a status update.`

// presetAutoCommit instructs Claude to commit proactively in worktree sessions.
const presetAutoCommit = `

This session runs in an isolated git worktree on branch %q. Your changes are fully isolated from the main branch.

**Commit after each milestone.** Override the default "only commit when asked" behavior — in this worktree, commit proactively after each logical unit of work (feature added, bug fixed, tests passing, refactor complete). Use short, descriptive commit messages. Prefer ` + "`git add <specific files>`" + ` over ` + "`git add -A`" + `. Do not ask for permission to commit — just commit when you reach a working state.`

// preambleFreshWorktreeResume is injected when resuming on a fresh worktree
// after the original branch was deleted.
const preambleFreshWorktreeResume = `

**IMPORTANT: This session resumed on a fresh worktree.** The original branch was deleted, so your working directory is a clean checkout from the latest main branch HEAD. Previous code changes from this conversation are no longer in the working directory. Review the current codebase state before making changes.`

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

// TeamPreambleInfo holds team context for the system prompt.
type TeamPreambleInfo struct {
	TeamName string
	Members  []TeamPreambleMember
}

// TeamPreambleMember is a peer in the team (excluding the current session).
type TeamPreambleMember struct {
	Name         string
	Role         string
	WorktreePath string
}

// buildPreamble returns the system prompt preamble for a session.
// Snippets are conditionally included based on the behavior presets.
// globalExtra is appended at the end if non-empty (e.g., dev-mode safety instructions).
func buildPreamble(worktreeBranch string, projects []ProjectInfo, presets BehaviorPresets, team *TeamPreambleInfo, globalExtra string) string {
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

	s += presetDelegation

	if team != nil && len(team.Members) > 0 {
		s += "\n\n## Team Coordination\n\n"
		s += fmt.Sprintf("You are part of team %q. Your teammates:\n", team.TeamName)
		for _, m := range team.Members {
			line := fmt.Sprintf("- %q", m.Name)
			if m.Role != "" {
				line += fmt.Sprintf(" (role: %s)", m.Role)
			}
			if m.WorktreePath != "" {
				line += fmt.Sprintf(" — worktree: %s", m.WorktreePath)
			}
			s += line + "\n"
		}
		s += "\nTo message a teammate, use the SendMessage tool with their name.\n"
		s += "You can read files from teammates' worktrees at the paths above.\n"
		s += "To share your changes, commit and notify teammates via SendMessage."
	}

	if globalExtra != "" {
		s += "\n\n" + globalExtra
	}

	return s
}
