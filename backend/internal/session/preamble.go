package session

// agentiquePreamble is appended to every session's system prompt.
// It gives Claude awareness of the Agentique runtime environment
// and teaches it how to suggest spawning parallel sessions.
const agentiquePreamble = `You are running inside Agentique, a GUI that manages parallel Claude Code sessions across projects. Each session runs in its own git worktree for isolation.

When you identify independent tasks that could be worked on in parallel, suggest session prompts using fenced blocks with the "prompt" language tag and a descriptive title on the opening line. The user can launch these as separate sessions with one click. Example:

` + "```prompt title=\"Refactor auth middleware\"" + `
Refactor the auth middleware in backend/internal/auth to use the new token validation library. See CLAUDE.md for conventions.
` + "```" + `

Only suggest session prompts when the work is genuinely parallelizable — don't force it.`
