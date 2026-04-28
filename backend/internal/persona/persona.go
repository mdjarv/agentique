package persona

import (
	"context"
	"fmt"
	"log/slog"
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
	Action        string  `json:"action"`
	Confidence    float64 `json:"confidence"`
	RedirectTo    string  `json:"redirectTo"`
	Reason        string  `json:"reason"`
	Response      string  `json:"response"`
	ResponseMs    int64   `json:"responseMs"`
	InteractionID string  `json:"interactionId"`
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
	Capabilities          []string
}

// GenerateProfileResult is the parsed profile suggestion.
type GenerateProfileResult struct {
	Name                  string   `json:"name"`
	Role                  string   `json:"role"`
	Description           string   `json:"description"`
	Avatar                string   `json:"avatar"`
	SystemPromptAdditions string   `json:"systemPromptAdditions"`
	CustomInstructions    string   `json:"customInstructions"`
	Capabilities          []string `json:"capabilities"`
	Config                string   `json:"config"` // JSON string of suggested behaviorPresets
}

// haikuRunOptions are the claudecli options shared by every persona Haiku call.
func haikuRunOptions() []claudecli.Option {
	return []claudecli.Option{
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	}
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
	result, err := msggen.RunWithRetry(ctx, s.runner, prompt, haikuRunOptions()...)
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

// GenerateProfile uses Haiku to suggest an agent profile based on project context.
func (s *Service) GenerateProfile(ctx context.Context, input GenerateProfileInput) (GenerateProfileResult, error) {
	prompt := buildProfilePrompt(input)

	result, err := msggen.RunWithRetry(ctx, s.runner, prompt, haikuRunOptions()...)
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
