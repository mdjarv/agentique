package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// TeamInfo is the wire type for team metadata sent to clients.
type TeamInfo struct {
	ID        string       `json:"id"`
	ProjectID string       `json:"projectId"`
	Name      string       `json:"name"`
	Members   []TeamMember `json:"members"`
	CreatedAt string       `json:"createdAt"`
}

// TeamMember is a lightweight member summary.
type TeamMember struct {
	SessionID    string `json:"sessionId"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	State        string `json:"state"`
	Connected    bool   `json:"connected"`
	WorktreePath string `json:"worktreePath,omitempty"`
}

// AgentMessagePayload is the payload for routing a message between sessions.
type AgentMessagePayload struct {
	SenderSessionID string `json:"senderSessionId"`
	TargetSessionID string `json:"targetSessionId"`
	Content         string `json:"content"`
}

// CreateTeam creates a team and broadcasts the creation.
func (s *Service) CreateTeam(ctx context.Context, projectID, name string) (TeamInfo, error) {
	teamID := uuid.New().String()
	team, err := s.queries.CreateTeam(ctx, store.CreateTeamParams{
		ID:        teamID,
		Name:      name,
		ProjectID: projectID,
	})
	if err != nil {
		return TeamInfo{}, fmt.Errorf("create team: %w", err)
	}

	info := TeamInfo{
		ID:        team.ID,
		ProjectID: team.ProjectID,
		Name:      team.Name,
		Members:   []TeamMember{},
		CreatedAt: team.CreatedAt,
	}
	s.hub.Broadcast(projectID, "team.created", info)
	return info, nil
}

// DeleteTeam removes a team, clears callbacks on live sessions, and unlinks all members.
func (s *Service) DeleteTeam(ctx context.Context, teamID string) error {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	// Clear agent message callbacks and team association on all members.
	members, _ := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	for _, m := range members {
		if live := s.mgr.Get(m.ID); live != nil {
			live.SetAgentMessageCallback(nil)
		}
		_ = s.queries.ClearSessionTeam(ctx, m.ID)
	}

	if err := s.queries.DeleteTeam(ctx, teamID); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}

	s.hub.Broadcast(team.ProjectID, "team.deleted", map[string]string{"teamId": teamID})
	return nil
}

// JoinTeam adds a session to a team, broadcasts the change, and returns the
// updated TeamInfo so the caller (RPC handler) can forward it to the client.
func (s *Service) JoinTeam(ctx context.Context, sessionID, teamID, role string) (TeamInfo, error) {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return TeamInfo{}, fmt.Errorf("team not found: %w", err)
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return TeamInfo{}, fmt.Errorf("session not found: %w", err)
	}

	// Reject duplicate names within the team.
	existingMembers, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return TeamInfo{}, fmt.Errorf("list team members: %w", err)
	}
	for _, m := range existingMembers {
		if m.Name == dbSess.Name && m.ID != sessionID {
			return TeamInfo{}, fmt.Errorf("team member named %q already exists; rename this session first", dbSess.Name)
		}
	}

	if err := s.queries.SetSessionTeam(ctx, store.SetSessionTeamParams{
		TeamID:   sql.NullString{String: teamID, Valid: true},
		TeamRole: role,
		ID:       sessionID,
	}); err != nil {
		return TeamInfo{}, fmt.Errorf("set session team: %w", err)
	}

	member := TeamMember{
		SessionID:    sessionID,
		Name:         dbSess.Name,
		Role:         role,
		State:        dbSess.State,
		Connected:    s.mgr.IsLive(sessionID),
		WorktreePath: nullStr(dbSess.WorktreePath),
	}

	info, buildErr := s.buildTeamInfo(ctx, team)
	broadcastPayload := map[string]any{
		"teamId": teamID,
		"member": member,
	}
	if buildErr == nil {
		broadcastPayload["team"] = info
	} else {
		slog.Warn("buildTeamInfo after join failed", "teamId", teamID, "error", buildErr)
	}
	s.hub.Broadcast(team.ProjectID, "team.member-joined", broadcastPayload)

	// Wire agent message callback and inject team context into a live session.
	if live := s.mgr.Get(sessionID); live != nil {
		s.wireAgentMessageCallback(live, teamID)
		go s.injectTeamContext(context.Background(), live, teamID)
	}

	return info, buildErr
}

// LeaveTeam removes a session from its team.
func (s *Service) LeaveTeam(ctx context.Context, sessionID string) error {
	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	teamID := nullStr(dbSess.TeamID)
	if teamID == "" {
		return nil
	}

	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	if err := s.queries.ClearSessionTeam(ctx, sessionID); err != nil {
		return fmt.Errorf("clear session team: %w", err)
	}

	// Clear agent message callback.
	if live := s.mgr.Get(sessionID); live != nil {
		live.SetAgentMessageCallback(nil)
	}

	s.hub.Broadcast(team.ProjectID, "team.member-left", map[string]any{
		"teamId":    teamID,
		"sessionId": sessionID,
	})
	return nil
}

// GetTeamInfo returns team metadata with members.
func (s *Service) GetTeamInfo(ctx context.Context, teamID string) (TeamInfo, error) {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return TeamInfo{}, fmt.Errorf("team not found: %w", err)
	}
	return s.buildTeamInfo(ctx, team)
}

// ListTeams returns all teams for a project.
func (s *Service) ListTeams(ctx context.Context, projectID string) ([]TeamInfo, error) {
	teams, err := s.queries.ListTeamsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}

	infos := make([]TeamInfo, 0, len(teams))
	for _, t := range teams {
		info, err := s.buildTeamInfo(ctx, t)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// RouteAgentMessage delivers a message from one session to another within the same team.
func (s *Service) RouteAgentMessage(ctx context.Context, p AgentMessagePayload) error {
	senderSess, err := s.queries.GetSession(ctx, p.SenderSessionID)
	if err != nil {
		return fmt.Errorf("sender not found: %w", err)
	}
	targetSess, err := s.queries.GetSession(ctx, p.TargetSessionID)
	if err != nil {
		return fmt.Errorf("target not found: %w", err)
	}

	senderTeamID := nullStr(senderSess.TeamID)
	targetTeamID := nullStr(targetSess.TeamID)
	if senderTeamID == "" || senderTeamID != targetTeamID {
		return fmt.Errorf("sender and target must be in the same team")
	}

	wireEvent := WireAgentMessageEvent{
		Type:            "agent_message",
		SenderSessionID: p.SenderSessionID,
		SenderName:      senderSess.Name,
		Content:         p.Content,
	}

	eventData, _ := json.Marshal(wireEvent)

	// Use the live session's turn/seq tracking, or fall back to 0/0
	// for offline sessions (will be visible on history reload).
	live := s.mgr.Get(p.TargetSessionID)
	turnIndex := int64(0)
	seq := int64(0)
	if live != nil {
		t, sq := live.pipeline.AllocSeq()
		turnIndex = int64(t)
		seq = int64(sq)
	}

	_ = s.queries.InsertEvent(ctx, store.InsertEventParams{
		SessionID: p.TargetSessionID,
		TurnIndex: turnIndex,
		Seq:       seq,
		Type:      "agent_message",
		Data:      string(eventData),
	})

	// Broadcast to frontend.
	s.hub.Broadcast(targetSess.ProjectID, "session.event", map[string]any{
		"sessionId": p.TargetSessionID,
		"event":     wireEvent,
	})

	// Deliver to the target's CLI via SendMessage.
	if live != nil {
		formatted := claudecli.FormatAgentMessage(senderSess.Name, p.Content)
		if err := live.cliSess.SendMessage(formatted); err != nil {
			slog.Warn("agent message CLI delivery failed", "target", p.TargetSessionID, "error", err)
		}
	}

	return nil
}

// GetTeamTimeline returns all agent messages across team members.
func (s *Service) GetTeamTimeline(ctx context.Context, teamID string) ([]WireAgentMessageEvent, error) {
	events, err := s.queries.ListAgentMessagesByTeam(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list agent messages: %w", err)
	}

	messages := make([]WireAgentMessageEvent, 0, len(events))
	for _, e := range events {
		var msg WireAgentMessageEvent
		if err := json.Unmarshal([]byte(e.Data), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (s *Service) buildTeamInfo(ctx context.Context, team store.Team) (TeamInfo, error) {
	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: team.ID, Valid: true})
	if err != nil {
		return TeamInfo{}, fmt.Errorf("list members: %w", err)
	}

	memberInfos := make([]TeamMember, 0, len(members))
	for _, m := range members {
		memberInfos = append(memberInfos, TeamMember{
			SessionID:    m.ID,
			Name:         m.Name,
			Role:         m.TeamRole,
			State:        m.State,
			Connected:    s.mgr.IsLive(m.ID),
			WorktreePath: nullStr(m.WorktreePath),
		})
	}

	return TeamInfo{
		ID:        team.ID,
		ProjectID: team.ProjectID,
		Name:      team.Name,
		Members:   memberInfos,
		CreatedAt: team.CreatedAt,
	}, nil
}

// buildTeamPreamble creates team context for the system prompt, excluding the given session.
func (s *Service) buildTeamPreamble(ctx context.Context, teamID, excludeSessionID string) *TeamPreambleInfo {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return nil
	}
	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return nil
	}
	var peers []TeamPreambleMember
	for _, m := range members {
		if m.ID == excludeSessionID {
			continue
		}
		peers = append(peers, TeamPreambleMember{
			Name:         m.Name,
			Role:         m.TeamRole,
			WorktreePath: nullStr(m.WorktreePath),
		})
	}
	if len(peers) == 0 {
		return nil
	}
	return &TeamPreambleInfo{
		TeamName: team.Name,
		Members:  peers,
	}
}

// wireAgentMessageCallback sets up the SendMessage interception callback on a
// live session. The callback resolves the target name to a session ID within
// the team and routes the message through RouteAgentMessage.
func (s *Service) wireAgentMessageCallback(sess *Session, teamID string) {
	sess.SetAgentMessageCallback(func(senderID, targetName, content string) error {
		members, err := s.queries.ListTeamMembers(context.Background(), sql.NullString{String: teamID, Valid: true})
		if err != nil {
			return fmt.Errorf("list team members: %w", err)
		}
		for _, m := range members {
			if m.Name == targetName {
				return s.RouteAgentMessage(context.Background(), AgentMessagePayload{
					SenderSessionID: senderID,
					TargetSessionID: m.ID,
					Content:         content,
				})
			}
		}
		return fmt.Errorf("no team member named %q", targetName)
	})
}

// injectTeamContext sends a message to a live session about its team peers.
func (s *Service) injectTeamContext(ctx context.Context, sess *Session, teamID string) {
	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return
	}

	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return
	}

	msg := fmt.Sprintf("You have joined team %q. Your teammates:\n", team.Name)
	for _, m := range members {
		if m.ID == sess.ID {
			continue
		}
		line := fmt.Sprintf("- %q", m.Name)
		if m.TeamRole != "" {
			line += fmt.Sprintf(" (role: %s)", m.TeamRole)
		}
		if wt := nullStr(m.WorktreePath); wt != "" {
			line += fmt.Sprintf(" — worktree: %s", wt)
		}
		msg += line + "\n"
	}
	msg += "\nTo message a teammate, use the SendMessage tool with their name.\n"
	msg += "You can read files from teammates' worktrees at the paths above."

	sess.mu.Lock()
	cli := sess.cliSess
	sess.mu.Unlock()
	if cli == nil {
		return
	}
	if err := cli.SendMessage(msg); err != nil {
		slog.Warn("team context injection failed", "session_id", sess.ID, "error", err)
	}
}
