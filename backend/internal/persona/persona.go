package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Broadcaster sends push messages to all connected WebSocket clients.
type Broadcaster interface {
	BroadcastAll(pushType string, payload any)
}

type serviceQueries interface {
	GetAgentProfile(ctx context.Context, id string) (store.AgentProfile, error)
	GetTeam(ctx context.Context, id string) (store.Team, error)
	ListTeamMembers(ctx context.Context, teamID string) ([]store.AgentProfile, error)
	InsertPersonaInteraction(ctx context.Context, arg store.InsertPersonaInteractionParams) (store.PersonaInteraction, error)
	ListPersonaInteractions(ctx context.Context, arg store.ListPersonaInteractionsParams) ([]store.PersonaInteraction, error)
	ListPersonaInteractionsForProfile(ctx context.Context, arg store.ListPersonaInteractionsForProfileParams) ([]store.PersonaInteraction, error)
}

// Service handles persona queries — stateless Haiku micro-agents that
// represent agent profiles when no full session is running.
type Service struct {
	runner  msggen.Runner
	queries serviceQueries
	hub     Broadcaster
}

// NewService creates a new persona service.
func NewService(runner msggen.Runner, queries serviceQueries, hub Broadcaster) *Service {
	return &Service{runner: runner, queries: queries, hub: hub}
}

// QueryInput describes a persona query.
type QueryInput struct {
	ProfileID string // target agent profile
	TeamID    string // team context
	AskerType string // "user" | "agent"
	AskerID   string // agent_profile_id if agent, empty if user
	AskerName string // display name of asker
	Question  string
}

// QueryResult is the parsed persona response.
type QueryResult struct {
	Action       string  `json:"action"`
	Confidence   float64 `json:"confidence"`
	RedirectTo   string  `json:"redirectTo"`
	Reason       string  `json:"reason"`
	Response     string  `json:"response"`
	ResponseMs   int64   `json:"responseMs"`
	InteractionID string `json:"interactionId"`
}

// InteractionInfo is the wire type sent to clients.
type InteractionInfo struct {
	ID             string  `json:"id"`
	ProfileID      string  `json:"profileId"`
	TeamID         string  `json:"teamId"`
	AskerType      string  `json:"askerType"`
	AskerID        string  `json:"askerId"`
	Question       string  `json:"question"`
	Action         string  `json:"action"`
	Confidence     float64 `json:"confidence"`
	Response       string  `json:"response"`
	RedirectTo     string  `json:"redirectTo"`
	ResponseTimeMs int64   `json:"responseTimeMs"`
	CreatedAt      string  `json:"createdAt"`
}

// Query runs a persona query: assembles context, calls Haiku, persists, broadcasts.
func (s *Service) Query(ctx context.Context, input QueryInput) (QueryResult, error) {
	profile, err := s.queries.GetAgentProfile(ctx, input.ProfileID)
	if err != nil {
		return QueryResult{}, fmt.Errorf("get agent profile: %w", err)
	}

	team, err := s.queries.GetTeam(ctx, input.TeamID)
	if err != nil {
		return QueryResult{}, fmt.Errorf("get team: %w", err)
	}

	members, err := s.queries.ListTeamMembers(ctx, input.TeamID)
	if err != nil {
		return QueryResult{}, fmt.Errorf("list team members: %w", err)
	}

	prompt := buildPrompt(profile, team, members, input)

	start := time.Now()
	result, err := msggen.RunWithRetry(ctx, s.runner, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	)
	elapsed := time.Since(start)

	if err != nil {
		return QueryResult{}, fmt.Errorf("haiku persona query failed: %w", err)
	}

	parsed := parseResponse(result.Text)
	parsed.ResponseMs = elapsed.Milliseconds()

	slog.Info("persona query completed",
		"profile_id", input.ProfileID,
		"profile_name", profile.Name,
		"team_id", input.TeamID,
		"asker_type", input.AskerType,
		"asker_name", input.AskerName,
		"action", parsed.Action,
		"confidence", parsed.Confidence,
		"redirect_to", parsed.RedirectTo,
		"response_ms", parsed.ResponseMs,
		"question_len", len(input.Question),
		"response_len", len(parsed.Response),
	)

	// Persist interaction.
	id := uuid.New().String()
	parsed.InteractionID = id
	row, err := s.queries.InsertPersonaInteraction(ctx, store.InsertPersonaInteractionParams{
		ID:             id,
		ProfileID:      input.ProfileID,
		TeamID:         input.TeamID,
		AskerType:      input.AskerType,
		AskerID:        input.AskerID,
		Question:       input.Question,
		Action:         parsed.Action,
		Confidence:     parsed.Confidence,
		Response:       parsed.Response,
		RedirectTo:     parsed.RedirectTo,
		ResponseTimeMs: parsed.ResponseMs,
	})
	if err != nil {
		slog.Warn("persist persona interaction failed", "error", err)
	} else {
		s.hub.BroadcastAll("persona.interaction", interactionInfoFromStore(row))
	}

	return parsed, nil
}

// ListInteractions returns recent persona interactions for a team.
func (s *Service) ListInteractions(ctx context.Context, teamID string, limit, offset int64) ([]InteractionInfo, error) {
	rows, err := s.queries.ListPersonaInteractions(ctx, store.ListPersonaInteractionsParams{
		TeamID: teamID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list persona interactions: %w", err)
	}
	out := make([]InteractionInfo, len(rows))
	for i, r := range rows {
		out[i] = interactionInfoFromStore(r)
	}
	return out, nil
}

// QueryForSession implements session.PersonaQuerier. It runs a persona query
// on behalf of a session's AskTeammate tool call.
func (s *Service) QueryForSession(ctx context.Context, profileName, teamID, askerProfileID, askerName, question string) (string, error) {
	// Look up profile by name within the team.
	members, err := s.queries.ListTeamMembers(ctx, teamID)
	if err != nil {
		return "", fmt.Errorf("list team members: %w", err)
	}

	var profileID string
	for _, m := range members {
		if m.Name == profileName {
			profileID = m.ID
			break
		}
	}
	if profileID == "" {
		return "", fmt.Errorf("no team member named %q", profileName)
	}

	result, err := s.Query(ctx, QueryInput{
		ProfileID: profileID,
		TeamID:    teamID,
		AskerType: "agent",
		AskerID:   askerProfileID,
		AskerName: askerName,
		Question:  question,
	})
	if err != nil {
		return "", err
	}
	return result.Response, nil
}

func interactionInfoFromStore(row store.PersonaInteraction) InteractionInfo {
	return InteractionInfo{
		ID:             row.ID,
		ProfileID:      row.ProfileID,
		TeamID:         row.TeamID,
		AskerType:      row.AskerType,
		AskerID:        row.AskerID,
		Question:       row.Question,
		Action:         row.Action,
		Confidence:     row.Confidence,
		Response:       row.Response,
		RedirectTo:     row.RedirectTo,
		ResponseTimeMs: row.ResponseTimeMs,
		CreatedAt:      row.CreatedAt,
	}
}

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

func parseResponse(text string) QueryResult {
	text = strings.TrimSpace(text)

	var result QueryResult
	result.Action = "answer"
	result.Confidence = 0.5

	lines := strings.Split(text, "\n")
	var responseLines []string
	inResponse := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if val, ok := strings.CutPrefix(trimmed, "ACTION:"); ok {
			result.Action = strings.ToLower(strings.TrimSpace(val))
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "CONFIDENCE:"); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				result.Confidence = f
			}
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "REDIRECT_TO:"); ok {
			result.RedirectTo = strings.TrimSpace(val)
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "REASON:"); ok {
			result.Reason = strings.TrimSpace(val)
			continue
		}
		if val, ok := strings.CutPrefix(trimmed, "RESPONSE:"); ok {
			inResponse = true
			rest := strings.TrimSpace(val)
			if rest != "" {
				responseLines = append(responseLines, rest)
			}
			continue
		}
		if inResponse {
			responseLines = append(responseLines, line)
		}
	}

	result.Response = strings.TrimSpace(strings.Join(responseLines, "\n"))

	// Fallback: if no structured response was parsed, use the full text.
	if result.Response == "" {
		result.Response = text
	}

	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Profile Generation ---

// GenerateProfileInput describes a profile generation request.
type GenerateProfileInput struct {
	ProjectName string
	ClaudeMD    string   // contents of CLAUDE.md (may be empty)
	FileTree    []string // git-tracked file paths
	Brief       string   // optional user-provided brief

	// Hints from the user's in-progress draft. Non-empty values are treated
	// as authoritative — the model must keep them verbatim and align the
	// remaining fields around them.
	Name                  string
	Role                  string
	Description           string
	Avatar                string
	SystemPromptAdditions string
	CustomInstructions    string
}

// GenerateProfileResult is the parsed profile suggestion.
type GenerateProfileResult struct {
	Name                  string `json:"name"`
	Role                  string `json:"role"`
	Description           string `json:"description"`
	Avatar                string `json:"avatar"`
	SystemPromptAdditions string `json:"systemPromptAdditions"`
	CustomInstructions    string `json:"customInstructions"`
	Config                string `json:"config"` // JSON string of suggested behaviorPresets
}

// GenerateProfile uses Haiku to suggest an agent profile based on project context.
func (s *Service) GenerateProfile(ctx context.Context, input GenerateProfileInput) (GenerateProfileResult, error) {
	prompt := buildProfilePrompt(input)

	result, err := msggen.RunWithRetry(ctx, s.runner, prompt,
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	)
	if err != nil {
		return GenerateProfileResult{}, fmt.Errorf("haiku profile generation failed: %w", err)
	}

	parsed := parseProfileResponse(result.Text)
	slog.Info("profile generated",
		"project", input.ProjectName,
		"name", parsed.Name,
		"role", parsed.Role,
		"brief_len", len(input.Brief),
	)
	return parsed, nil
}

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
		input.Avatar != "" || input.SystemPromptAdditions != "" || input.CustomInstructions != ""
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
	b.WriteString("AVATAR: <single emoji>\n")
	b.WriteString("SYSTEM_PROMPT: <3-6 sentences appended to every session preamble. Define the agent's voice, priorities, and guardrails. Written as direct instructions (\"You are...\", \"Always...\"). Leave the line blank after the colon if nothing meaningful to add.>\n")
	b.WriteString("CUSTOM_INSTRUCTIONS: <optional 1-3 sentences of preset-level tweaks like \"only touch backend files\". Leave blank if none.>\n")
	b.WriteString("CONFIG: <JSON with behaviorPresets only, e.g. {\"autoCommit\": true, \"suggestParallel\": false, \"planFirst\": false, \"terse\": true}>\n")

	return b.String()
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

func parseProfileResponse(text string) GenerateProfileResult {
	text = strings.TrimSpace(text)
	var result GenerateProfileResult

	var descLines, systemPromptLines, customInstLines []string
	// current tracks which multi-line field we're accumulating into.
	// "" means we're not inside a multi-line field.
	current := ""

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case matchField(trimmed, "NAME:", &result.Name):
			current = ""
		case matchField(trimmed, "ROLE:", &result.Role):
			current = ""
		case matchField(trimmed, "AVATAR:", &result.Avatar):
			current = ""
		case matchField(trimmed, "CONFIG:", &result.Config):
			current = ""
		case startsMultiline(trimmed, "DESCRIPTION:", &descLines):
			current = "description"
		case startsMultiline(trimmed, "SYSTEM_PROMPT:", &systemPromptLines):
			current = "systemPrompt"
		case startsMultiline(trimmed, "CUSTOM_INSTRUCTIONS:", &customInstLines):
			current = "customInst"
		default:
			switch current {
			case "description":
				descLines = append(descLines, line)
			case "systemPrompt":
				systemPromptLines = append(systemPromptLines, line)
			case "customInst":
				customInstLines = append(customInstLines, line)
			}
		}
	}

	result.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
	result.SystemPromptAdditions = strings.TrimSpace(strings.Join(systemPromptLines, "\n"))
	result.CustomInstructions = strings.TrimSpace(strings.Join(customInstLines, "\n"))

	// Validate CONFIG is valid JSON; fall back to "{}" on parse failure.
	if result.Config != "" {
		var tmp map[string]any
		if json.Unmarshal([]byte(result.Config), &tmp) != nil {
			result.Config = "{}"
		}
	} else {
		result.Config = "{}"
	}

	return result
}

// matchField returns true and writes into dst when trimmed starts with prefix.
// Used for single-line fields (NAME/ROLE/AVATAR/CONFIG).
func matchField(trimmed, prefix string, dst *string) bool {
	val, ok := strings.CutPrefix(trimmed, prefix)
	if !ok {
		return false
	}
	*dst = strings.TrimSpace(val)
	return true
}

// startsMultiline returns true and seeds lines with any content on the
// same line as the label, when trimmed begins with prefix.
func startsMultiline(trimmed, prefix string, lines *[]string) bool {
	val, ok := strings.CutPrefix(trimmed, prefix)
	if !ok {
		return false
	}
	if rest := strings.TrimSpace(val); rest != "" {
		*lines = append(*lines, rest)
	}
	return true
}

// formatFileTree produces a compact tree representation grouped by top-level directory.
func formatFileTree(files []string, maxFiles int) string {
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	if len(files) == 0 {
		return "(empty)\n"
	}

	// Group by top-level directory.
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
		// Track second-level dirs.
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

		// Show key root-level files in this dir.
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

		// Show subdirectories.
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
