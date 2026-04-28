package persona

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// buildPrompt assembles the persona-Query prompt sent to Haiku.
func buildPrompt(profile store.AgentProfile, team store.Team, members []store.AgentProfile, input QueryInput) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are the persona of %q, a %s on the %q team.\n\n", profile.Name, profile.Role, team.Name)

	b.WriteString("## Your Identity\n")
	b.WriteString(profile.Description)
	b.WriteString("\n\n")

	b.WriteString("## Your Teammates\n")
	for _, m := range members {
		if m.ID == profile.ID {
			continue // skip self
		}
		fmt.Fprintf(&b, "- %q", m.Name)
		if m.Role != "" {
			fmt.Fprintf(&b, " (role: %s)", m.Role)
		}
		if m.Description != "" {
			fmt.Fprintf(&b, " — %s", truncate(m.Description, 200))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString("## Question\n")
	askerLabel := "A user"
	if input.AskerName != "" {
		askerLabel = fmt.Sprintf("%q (a teammate)", input.AskerName)
	}
	fmt.Fprintf(&b, "%s asks: %s\n\n", askerLabel, input.Question)

	b.WriteString("## Instructions\n\n")
	b.WriteString("Evaluate the question and respond. Choose the most appropriate action:\n\n")
	b.WriteString("- **answer** — You can answer directly (capability questions, status, knowledge). No full session needed.\n")
	b.WriteString("- **spawn** — This requires your full attention (work requests, bugs, complex tasks). Recommend spawning a session.\n")
	b.WriteString("- **queue** — Informational/FYI. Not urgent. Queue for later.\n")
	b.WriteString("- **reject** — Not your domain. Can't help.\n")
	b.WriteString("- **redirect** — Another teammate is better suited. Name them.\n\n")

	b.WriteString("Respond in EXACTLY this format with no other text:\n")
	b.WriteString("ACTION: <action>\n")
	b.WriteString("CONFIDENCE: <0.0-1.0>\n")
	b.WriteString("REDIRECT_TO: <teammate name or empty>\n")
	b.WriteString("REASON: <one line>\n\n")
	b.WriteString("RESPONSE: <your natural language answer to the caller>\n")

	return b.String()
}

// buildProfilePrompt assembles the GenerateProfile prompt sent to Haiku.
func buildProfilePrompt(input GenerateProfileInput) string {
	var b strings.Builder

	b.WriteString("You are an expert at designing AI agent profiles for software development teams.\n")
	b.WriteString("Given a project's context, suggest a single specialized agent profile.\n\n")

	fmt.Fprintf(&b, "## Project: %s\n\n", input.ProjectName)

	if input.ClaudeMD != "" {
		b.WriteString("## Project Guidelines (from CLAUDE.md)\n")
		b.WriteString(truncate(input.ClaudeMD, 4000))
		b.WriteString("\n\n")
	}

	b.WriteString("## Repository Structure\n")
	b.WriteString(formatFileTree(input.FileTree, 200))
	b.WriteString("\n\n")

	if input.Brief != "" {
		b.WriteString("## User's Request\n")
		b.WriteString(input.Brief)
		b.WriteString("\n\n")
	}

	hasDraft := input.Name != "" || input.Role != "" || input.Description != "" ||
		input.Avatar != "" || input.SystemPromptAdditions != "" || input.CustomInstructions != "" ||
		len(input.Capabilities) > 0
	if hasDraft {
		b.WriteString("## User's Draft (authoritative — keep verbatim)\n")
		b.WriteString(
			"The user has already filled some fields. Treat each non-empty value below as a binding constraint: ",
		)
		b.WriteString(
			"echo it back unchanged and shape the remaining fields around it. ",
		)
		b.WriteString(
			"For example, if ROLE is \"Architect\" the rest of the profile must describe an architect persona.\n\n",
		)
		if input.Name != "" {
			fmt.Fprintf(&b, "- NAME: %s\n", input.Name)
		}
		if input.Role != "" {
			fmt.Fprintf(&b, "- ROLE: %s\n", input.Role)
		}
		if input.Description != "" {
			fmt.Fprintf(&b, "- DESCRIPTION: %s\n", input.Description)
		}
		if input.Avatar != "" {
			fmt.Fprintf(&b, "- AVATAR: %s\n", input.Avatar)
		}
		if input.SystemPromptAdditions != "" {
			fmt.Fprintf(&b, "- SYSTEM_PROMPT:\n%s\n", indent(input.SystemPromptAdditions, "  "))
		}
		if input.CustomInstructions != "" {
			fmt.Fprintf(&b, "- CUSTOM_INSTRUCTIONS:\n%s\n", indent(input.CustomInstructions, "  "))
		}
		if len(input.Capabilities) > 0 {
			fmt.Fprintf(&b, "- CAPABILITIES: %s\n", strings.Join(input.Capabilities, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Task\n\n")
	b.WriteString("Generate a suggested agent profile. Consider:\n")
	b.WriteString("- The project's primary language and framework (inferred from files)\n")
	b.WriteString("- Key conventions from CLAUDE.md if available\n")
	b.WriteString("- The user's request if provided\n")
	if hasDraft {
		b.WriteString(
			"- The user's draft above — any field the user already filled MUST be echoed verbatim; only generate the missing ones and keep them consistent with the draft\n",
		)
	}
	b.WriteString("- What kind of specialist would be most productive on this codebase\n\n")

	b.WriteString("Respond in EXACTLY this format. Each field starts on its own line with its label. ")
	b.WriteString("Multi-line fields (DESCRIPTION, SYSTEM_PROMPT, CUSTOM_INSTRUCTIONS) continue until the next label. ")
	b.WriteString("No other text before, between, or after the fields.\n\n")
	b.WriteString("NAME: <2-3 word agent name>\n")
	b.WriteString("ROLE: <concise role, e.g. \"backend architect\" or \"fullstack developer\">\n")
	b.WriteString("DESCRIPTION: <2-4 sentences about expertise, focus areas, and working style. Reference specific technologies from the project.>\n")
	b.WriteString("AVATAR: <one simple, single-codepoint emoji such as 🤖 🧠 🔧 🔍 📝 📊 🎨 🚀 ⚡ 💻 🎯 🧙 🦉 🦊 🐙 🦖. No variation selectors (U+FE0F), no ZWJ sequences, no skin-tone modifiers.>\n")
	b.WriteString("SYSTEM_PROMPT: <3-6 sentences appended to every session preamble. Define the agent's voice, priorities, and guardrails. Written as direct instructions (\"You are...\", \"Always...\"). Leave the line blank after the colon if nothing meaningful to add.>\n")
	b.WriteString("CUSTOM_INSTRUCTIONS: <optional 1-3 sentences of preset-level tweaks like \"only touch backend files\". Leave blank if none.>\n")
	b.WriteString("CAPABILITIES: <comma-separated list of 3-6 short kebab-case tags describing what this agent does well, e.g. \"go-backend, sqlc-migrations, channel-routing\". Used by teammates to route work.>\n")
	b.WriteString("CONFIG: <JSON with behaviorPresets only, e.g. {\"autoCommit\": true, \"suggestParallel\": false, \"planFirst\": false, \"terse\": true}>\n")

	return b.String()
}

// truncate clips s to maxLen, appending "..." when shortened.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// indent prefixes every line of s with prefix.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// formatFileTree produces a compact tree representation grouped by top-level directory.
func formatFileTree(files []string, maxFiles int) string {
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	if len(files) == 0 {
		return "(empty)\n"
	}

	type dirInfo struct {
		files []string
		dirs  map[string]int // subdirectory → file count
	}
	groups := make(map[string]*dirInfo)
	var rootFiles []string
	var dirOrder []string

	for _, f := range files {
		topDir := strings.SplitN(f, "/", 2)[0]
		if !strings.Contains(f, "/") {
			rootFiles = append(rootFiles, f)
			continue
		}
		di, ok := groups[topDir]
		if !ok {
			di = &dirInfo{dirs: make(map[string]int)}
			groups[topDir] = di
			dirOrder = append(dirOrder, topDir)
		}
		di.files = append(di.files, f)
		rest := f[len(topDir)+1:]
		if subDir := path.Dir(rest); subDir != "." {
			parts := strings.SplitN(subDir, "/", 2)
			di.dirs[parts[0]]++
		}
	}
	sort.Strings(dirOrder)

	var b strings.Builder
	for _, d := range dirOrder {
		di := groups[d]
		fmt.Fprintf(&b, "%s/ (%d files)\n", d, len(di.files))

		var keyFiles []string
		for _, f := range di.files {
			rel := f[len(d)+1:]
			if !strings.Contains(rel, "/") {
				keyFiles = append(keyFiles, rel)
			}
		}
		if len(keyFiles) > 0 && len(keyFiles) <= 8 {
			fmt.Fprintf(&b, "  %s\n", strings.Join(keyFiles, ", "))
		}

		if len(di.dirs) > 0 {
			subDirs := make([]string, 0, len(di.dirs))
			for sd := range di.dirs {
				subDirs = append(subDirs, sd)
			}
			sort.Strings(subDirs)
			if len(subDirs) <= 10 {
				fmt.Fprintf(&b, "  %s/\n", strings.Join(subDirs, "/, "))
			} else {
				fmt.Fprintf(&b, "  %s/, ... (%d more)\n", strings.Join(subDirs[:8], "/, "), len(subDirs)-8)
			}
		}
	}

	if len(rootFiles) > 0 {
		sort.Strings(rootFiles)
		for _, f := range rootFiles {
			fmt.Fprintf(&b, "%s\n", f)
		}
	}

	return b.String()
}
