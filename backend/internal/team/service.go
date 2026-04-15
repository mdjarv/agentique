package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Broadcaster sends push messages to all connected WebSocket clients.
type Broadcaster interface {
	BroadcastAll(pushType string, payload any)
}

type serviceQueries interface {
	// Agent profiles
	CreateAgentProfile(ctx context.Context, arg store.CreateAgentProfileParams) (store.AgentProfile, error)
	GetAgentProfile(ctx context.Context, id string) (store.AgentProfile, error)
	ListAgentProfiles(ctx context.Context) ([]store.AgentProfile, error)
	UpdateAgentProfile(ctx context.Context, arg store.UpdateAgentProfileParams) (store.AgentProfile, error)
	DeleteAgentProfile(ctx context.Context, id string) error

	// Teams
	CreateTeam(ctx context.Context, arg store.CreateTeamParams) (store.Team, error)
	GetTeam(ctx context.Context, id string) (store.Team, error)
	ListTeams(ctx context.Context) ([]store.Team, error)
	UpdateTeam(ctx context.Context, arg store.UpdateTeamParams) (store.Team, error)
	DeleteTeam(ctx context.Context, id string) error
	AddTeamMember(ctx context.Context, arg store.AddTeamMemberParams) error
	RemoveTeamMember(ctx context.Context, arg store.RemoveTeamMemberParams) error
	ListTeamMembers(ctx context.Context, teamID string) ([]store.AgentProfile, error)
}

// Service manages persistent agent profiles and teams.
type Service struct {
	queries serviceQueries
	hub     Broadcaster
}

// NewService creates a new team service.
func NewService(queries serviceQueries, hub Broadcaster) *Service {
	return &Service{queries: queries, hub: hub}
}

// --- Agent Profile Config ---

// AgentProfileConfig holds session-creation defaults for an agent profile.
type AgentProfileConfig struct {
	Model           string          `json:"model,omitempty"`
	PermissionMode  string          `json:"permissionMode,omitempty"`
	AutoApproveMode string          `json:"autoApproveMode,omitempty"`
	Effort          string          `json:"effort,omitempty"`
	BehaviorPresets BehaviorPresets `json:"behaviorPresets"`
}

// BehaviorPresets mirrors session.BehaviorPresets for agent profile config.
type BehaviorPresets struct {
	AutoCommit         bool   `json:"autoCommit"`
	SuggestParallel    bool   `json:"suggestParallel"`
	PlanFirst          bool   `json:"planFirst"`
	Terse              bool   `json:"terse"`
	CustomInstructions string `json:"customInstructions,omitempty"`
}

// --- Wire types ---

// AgentProfileInfo is the wire type sent to clients.
type AgentProfileInfo struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Role        string             `json:"role"`
	Description string             `json:"description"`
	ProjectID   string             `json:"projectId"`
	Avatar      string             `json:"avatar"`
	Config      AgentProfileConfig `json:"config"`
	CreatedAt   string             `json:"createdAt"`
	UpdatedAt   string             `json:"updatedAt"`
}

// TeamInfo is the wire type for a team with its members.
type TeamInfo struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Members     []AgentProfileInfo `json:"members"`
	CreatedAt   string             `json:"createdAt"`
	UpdatedAt   string             `json:"updatedAt"`
}

// --- Agent Profile CRUD ---

// CreateAgentProfile creates a new agent profile.
func (s *Service) CreateAgentProfile(ctx context.Context, name, role, description, projectID, avatar, configJSON string) (AgentProfileInfo, error) {
	id := uuid.New().String()
	row, err := s.queries.CreateAgentProfile(ctx, store.CreateAgentProfileParams{
		ID:          id,
		Name:        name,
		Role:        role,
		Description: description,
		ProjectID:   toNullString(projectID),
		Avatar:      avatar,
		Config:      configJSON,
	})
	if err != nil {
		return AgentProfileInfo{}, fmt.Errorf("create agent profile: %w", err)
	}
	info := profileInfoFromStore(row)
	s.hub.BroadcastAll("agent-profile.created", info)
	return info, nil
}

// UpdateAgentProfile updates an existing agent profile.
func (s *Service) UpdateAgentProfile(ctx context.Context, id, name, role, description, projectID, avatar, configJSON string) (AgentProfileInfo, error) {
	row, err := s.queries.UpdateAgentProfile(ctx, store.UpdateAgentProfileParams{
		Name:        name,
		Role:        role,
		Description: description,
		ProjectID:   toNullString(projectID),
		Avatar:      avatar,
		Config:      configJSON,
		ID:          id,
	})
	if err != nil {
		return AgentProfileInfo{}, fmt.Errorf("update agent profile: %w", err)
	}
	info := profileInfoFromStore(row)
	s.hub.BroadcastAll("agent-profile.updated", info)
	return info, nil
}

// DeleteAgentProfile deletes an agent profile by ID.
func (s *Service) DeleteAgentProfile(ctx context.Context, id string) error {
	if err := s.queries.DeleteAgentProfile(ctx, id); err != nil {
		return fmt.Errorf("delete agent profile: %w", err)
	}
	s.hub.BroadcastAll("agent-profile.deleted", map[string]string{"id": id})
	return nil
}

// ListAgentProfiles returns all agent profiles.
func (s *Service) ListAgentProfiles(ctx context.Context) ([]AgentProfileInfo, error) {
	rows, err := s.queries.ListAgentProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	out := make([]AgentProfileInfo, len(rows))
	for i, r := range rows {
		out[i] = profileInfoFromStore(r)
	}
	return out, nil
}

// --- Team CRUD ---

// CreateTeam creates a new team.
func (s *Service) CreateTeam(ctx context.Context, name, description string) (TeamInfo, error) {
	id := uuid.New().String()
	row, err := s.queries.CreateTeam(ctx, store.CreateTeamParams{
		ID:          id,
		Name:        name,
		Description: description,
	})
	if err != nil {
		return TeamInfo{}, fmt.Errorf("create team: %w", err)
	}
	info := teamInfoFromStore(row, nil)
	s.hub.BroadcastAll("team.created", info)
	return info, nil
}

// UpdateTeam updates an existing team.
func (s *Service) UpdateTeam(ctx context.Context, id, name, description string) (TeamInfo, error) {
	row, err := s.queries.UpdateTeam(ctx, store.UpdateTeamParams{
		Name:        name,
		Description: description,
		ID:          id,
	})
	if err != nil {
		return TeamInfo{}, fmt.Errorf("update team: %w", err)
	}
	members, _ := s.listTeamMemberInfos(ctx, id)
	info := teamInfoFromStore(row, members)
	s.hub.BroadcastAll("team.updated", info)
	return info, nil
}

// DeleteTeam deletes a team by ID.
func (s *Service) DeleteTeam(ctx context.Context, id string) error {
	if err := s.queries.DeleteTeam(ctx, id); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	s.hub.BroadcastAll("team.deleted", map[string]string{"id": id})
	return nil
}

// ListTeams returns all teams with their members.
func (s *Service) ListTeams(ctx context.Context) ([]TeamInfo, error) {
	rows, err := s.queries.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	out := make([]TeamInfo, len(rows))
	for i, r := range rows {
		members, _ := s.listTeamMemberInfos(ctx, r.ID)
		out[i] = teamInfoFromStore(r, members)
	}
	return out, nil
}

// --- Team Membership ---

// AddTeamMember adds an agent profile to a team.
func (s *Service) AddTeamMember(ctx context.Context, teamID, agentProfileID string, sortOrder int) (TeamInfo, error) {
	if err := s.queries.AddTeamMember(ctx, store.AddTeamMemberParams{
		TeamID:         teamID,
		AgentProfileID: agentProfileID,
		SortOrder:      int64(sortOrder),
	}); err != nil {
		return TeamInfo{}, fmt.Errorf("add team member: %w", err)
	}
	return s.getTeamInfo(ctx, teamID)
}

// RemoveTeamMember removes an agent profile from a team.
func (s *Service) RemoveTeamMember(ctx context.Context, teamID, agentProfileID string) (TeamInfo, error) {
	if err := s.queries.RemoveTeamMember(ctx, store.RemoveTeamMemberParams{
		TeamID:         teamID,
		AgentProfileID: agentProfileID,
	}); err != nil {
		return TeamInfo{}, fmt.Errorf("remove team member: %w", err)
	}
	return s.getTeamInfo(ctx, teamID)
}

// --- Helpers ---

func (s *Service) getTeamInfo(ctx context.Context, teamID string) (TeamInfo, error) {
	row, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return TeamInfo{}, fmt.Errorf("get team: %w", err)
	}
	members, _ := s.listTeamMemberInfos(ctx, teamID)
	info := teamInfoFromStore(row, members)
	s.hub.BroadcastAll("team.updated", info)
	return info, nil
}

func (s *Service) listTeamMemberInfos(ctx context.Context, teamID string) ([]AgentProfileInfo, error) {
	rows, err := s.queries.ListTeamMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentProfileInfo, len(rows))
	for i, r := range rows {
		out[i] = profileInfoFromStore(r)
	}
	return out, nil
}

func profileInfoFromStore(row store.AgentProfile) AgentProfileInfo {
	var cfg AgentProfileConfig
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		slog.Warn("malformed agent profile config", "profile_id", row.ID, "error", err)
	}

	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.String
	}

	return AgentProfileInfo{
		ID:          row.ID,
		Name:        row.Name,
		Role:        row.Role,
		Description: row.Description,
		ProjectID:   projectID,
		Avatar:      row.Avatar,
		Config:      cfg,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func teamInfoFromStore(row store.Team, members []AgentProfileInfo) TeamInfo {
	if members == nil {
		members = []AgentProfileInfo{}
	}
	return TeamInfo{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Members:     members,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
