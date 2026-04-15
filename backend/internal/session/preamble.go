package session

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// preambleIdentity is always emitted — establishes Agentique context.
const preambleIdentity = `You are running inside Agentique, a GUI that manages parallel Claude Code sessions across projects. Each session runs in its own git worktree for isolation.

When reporting to the user, be extremely concise — sacrifice grammar for brevity.`

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

You can create a channel of specialist workers, each running in its own git worktree. Use this when a task naturally decomposes into independent subtasks that different experts could tackle in parallel (e.g., "backend API + frontend UI + tests", or "migrate 3 services"). Don't spawn workers for sequential work or when you can do it faster yourself.

**How to think about channels:**
1. Identify 2-5 genuinely independent subtasks.
2. Give each worker a clear expert role and a self-contained prompt that includes all necessary context (file paths, conventions, interfaces to conform to). Workers cannot see your conversation — their prompt is all they know.
3. After spawning, wait for workers to report back, then synthesize their results, resolve conflicts between worktrees, and report the outcome to the user.

To spawn workers, use SendMessage with target ` + "`@spawn`" + `:

` + "```" + `
SendMessage({to: "@spawn", message: JSON.stringify({
  channelName: "descriptive channel name",
  workers: [
    {name: "Backend API", role: "backend expert", prompt: "Implement the REST endpoints for user profiles. The schema is in db/schema.sql. Follow conventions in CLAUDE.md. When done, commit your changes and message the lead with a summary of endpoints created and any interface decisions."},
    {name: "Frontend UI", role: "frontend expert", prompt: "Build the user profile page using the existing component patterns in src/components/. The API will provide GET/PUT /api/profiles/:id. When done, commit and message the lead with your component structure."}
  ]
})})
` + "```" + `

The user must approve before workers are created. Workers join your channel and communicate via SendMessage.

Workers use a ` + "`type`" + ` field in SendMessage to signal their status:
- **plan:** Worker's initial plan before starting — wait for all workers to check in.
- **progress:** Status update after a commit. The worker is still working — do NOT synthesize yet.
- **done:** Final report. Only synthesize results after ALL workers have sent a "done" message.

Messages arrive with a [PLAN], [PROGRESS], or [DONE] prefix corresponding to the type.

If a worker seems to be taking too long between updates, send them a message asking for a status update.`

// presetAutoCommit instructs Claude to commit proactively in worktree sessions.
const presetAutoCommit = `

This session runs in an isolated git worktree on branch %q. Your changes are fully isolated from the main branch.

**Commit after each milestone.** Override the default "only commit when asked" behavior — in this worktree, commit proactively after each logical unit of work (feature added, bug fixed, tests passing, refactor complete). Use short, descriptive commit messages. Prefer ` + "`git add <specific files>`" + ` over ` + "`git add -A`" + `. Do not ask for permission to commit — just commit when you reach a working state.`

// preambleFreshWorktreeResume is injected when resuming on a fresh worktree
// after the original branch was deleted.
const preambleFreshWorktreeResume = `

**IMPORTANT: This session resumed on a fresh worktree.** The original branch was deleted, so your working directory is a clean checkout from the latest main branch HEAD. Previous code changes from this conversation are no longer in the working directory. Review the current codebase state before making changes.`

// preambleConversationReset is injected when the user resets the conversation
// (clears claude_session_id). The new CLI has no history, so we instruct Claude
// to orient itself from the git state.
const preambleConversationReset = `

**IMPORTANT: This is a fresh conversation for an existing session.** The previous conversation history was reset by the user (likely because it became too large or unresponsive). Your code changes are still on disk — nothing was lost.

Before responding to the user's first message, quickly orient yourself:
1. Run ` + "`git log --oneline -20`" + ` to see recent commits on this branch
2. Run ` + "`git diff --stat HEAD`" + ` to see any uncommitted changes
3. Check if this is a worktree (` + "`git worktree list`" + `)

Use this context to understand what was being worked on, then proceed with the user's request.`

// presetPlanFirst instructs Claude to outline a plan before implementing.
const presetPlanFirst = `

**Plan before implementing.** Before writing code, outline your approach: what you'll change, which files are involved, and any trade-offs. Wait for confirmation before proceeding. This is a soft guideline — use judgment for trivial changes.`

// preambleAskTeammate instructs Claude how to query teammate personas.
// Only injected when the session is bound to a team.
const preambleAskTeammate = `

## Teammate Discovery

You can query your teammates without waking them up:
  AskTeammate({name: "Teammate Name", question: "your question"})
This asks their persona (a lightweight proxy) and returns immediately.
Use this for discovery and quick questions before sending full work requests via SendMessage.`

// presetTerse instructs Claude to minimize output.
const presetTerse = `

**Terse mode.** Be extremely concise. Skip explanations unless asked. Show code changes directly. No summaries, no preamble, no sign-offs.`

// preambleSessionFiles instructs Claude how to share images/files with the user.
// Always included — this is a core capability.
const preambleSessionFiles = `

## Session Files

The user views your responses in a web UI that renders inline images. To show the user an image (screenshot, diagram, export), you MUST serve it through the API — the UI cannot access local paths or other ports.

Session files directory (persistent, survives worktree deletion):

    %s

**Workflow:**
1. Save or copy the file to the session files directory above.
2. Embed it using ONLY this markdown pattern (no other URLs or paths will work):

       ![description](/api/sessions/%s/files/filename.png)

**IMPORTANT:** Never link to ` + "`localhost`" + `, ` + "`127.0.0.1`" + `, or any other host/port directly. The ONLY way the user can see files is through ` + "`/api/sessions/`" + `.

**Playwright screenshots:** After ` + "`mcp__agentique-playwright__browser_take_screenshot`" + `, the tool saves to a temp path. Copy that file to the session files directory, then embed using the pattern above.`

// preambleBrowser explains the managed browser and its MCP tools.
// Only included when the browser feature is enabled.
const preambleBrowser = `

## Browser

This session has a managed headless Chrome instance available via the ` + "`agentique-playwright`" + ` MCP server. Tools are prefixed ` + "`mcp__agentique-playwright__`" + ` (e.g. ` + "`mcp__agentique-playwright__browser_navigate`" + `). The user can see the browser in a panel beside the chat.

**Important:** Do NOT use these tools until you receive a message confirming the browser has been launched. The browser starts on demand — calling tools before launch will fail.

If a global Playwright plugin (` + "`mcp__playwright__*`" + `) is also available, prefer the ` + "`agentique-playwright`" + ` tools — they connect to the shared browser visible in the UI panel.`

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

// ChannelPreambleInfo holds channel context for the system prompt.
type ChannelPreambleInfo struct {
	ChannelName string
	Members     []ChannelPreambleMember
}

// ChannelPreambleMember is a peer in the channel (excluding the current session).
type ChannelPreambleMember struct {
	Name         string
	Role         string
	WorktreePath string
}

// TeamPreambleInfo holds team context for the system prompt.
type TeamPreambleInfo struct {
	TeamName    string
	ProfileName string
	ProfileRole string
	Teammates   []TeamPreambleMember
}

// TeamPreambleMember is a teammate in the team roster.
type TeamPreambleMember struct {
	Name string
	Role string
}

// buildPreamble returns the system prompt preamble for a session.
// Snippets are conditionally included based on the behavior presets.
// globalExtra is appended at the end if non-empty (e.g., dev-mode safety instructions).
func buildPreamble(sessionID, worktreeBranch string, projects []ProjectInfo, presets BehaviorPresets, channels []*ChannelPreambleInfo, teams []*TeamPreambleInfo, globalExtra string, browserEnabled bool) string {
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

	for _, channel := range channels {
		if channel == nil || len(channel.Members) == 0 {
			continue
		}
		s += "\n\n## Channel Coordination\n\n"
		s += fmt.Sprintf("You are part of channel %q. Your teammates:\n", channel.ChannelName)
		for _, m := range channel.Members {
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

	hasTeam := false
	for _, t := range teams {
		if t == nil || len(t.Teammates) == 0 {
			continue
		}
		hasTeam = true
		s += "\n\n## Team Identity\n\n"
		s += fmt.Sprintf("You are %q (role: %s) on team %q. Your teammates:\n", t.ProfileName, t.ProfileRole, t.TeamName)
		for _, m := range t.Teammates {
			line := fmt.Sprintf("- %q", m.Name)
			if m.Role != "" {
				line += fmt.Sprintf(" (role: %s)", m.Role)
			}
			s += line + "\n"
		}
	}

	if hasTeam {
		s += preambleAskTeammate
	}

	if browserEnabled {
		s += preambleBrowser
	}

	if sessionID != "" {
		filesDir := filepath.Join(paths.SessionFilesDir(), sessionID)
		s += fmt.Sprintf(preambleSessionFiles, filesDir, sessionID)
	}

	if globalExtra != "" {
		s += "\n\n" + globalExtra
	}

	return s
}
