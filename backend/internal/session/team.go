package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/gitops"
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
// Does NOT clean up worker sessions/worktrees/branches — use DissolveTeam for that.
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

// DissolveTeam stops all non-lead worker sessions, removes their worktrees and
// branches (force-delete), deletes them from DB, unlinks the leader, and deletes
// the team. The leader session stays alive as a normal session.
func (s *Service) DissolveTeam(ctx context.Context, teamID string) error {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	project, projErr := s.queries.GetProject(ctx, team.ProjectID)

	for _, m := range members {
		if m.TeamRole == "lead" {
			// Unlink lead — keep session alive.
			if live := s.mgr.Get(m.ID); live != nil {
				live.SetAgentMessageCallback(nil)
			}
			_ = s.queries.ClearSessionTeam(ctx, m.ID)
			continue
		}

		// Stop worker CLI process.
		if live := s.mgr.Get(m.ID); live != nil {
			live.SetAgentMessageCallback(nil)
			_ = s.mgr.Stop(ctx, m.ID)
		}

		// Remove worktree and force-delete branch.
		if projErr == nil {
			if wtPath := nullStr(m.WorktreePath); wtPath != "" {
				gitops.RemoveWorktree(project.Path, wtPath)
			}
			if branch := nullStr(m.WorktreeBranch); branch != "" {
				if delErr := gitops.ForceDeleteBranch(project.Path, branch); delErr != nil {
					slog.Warn("dissolve: branch force-delete failed",
						"session_id", m.ID, "branch", branch, "error", delErr)
				}
				gitops.DeleteRemoteBranch(project.Path, branch)
			}
		}

		// Delete session from DB.
		if err := s.queries.DeleteSession(ctx, m.ID); err != nil {
			slog.Warn("dissolve: session delete failed", "session_id", m.ID, "error", err)
			continue
		}
		if s.gitSvc != nil {
			s.gitSvc.CleanupVersion(m.ID)
		}
		s.hub.Broadcast(team.ProjectID, "session.deleted", map[string]any{
			"sessionId": m.ID,
		})
	}

	// Delete team record.
	if err := s.queries.DeleteTeam(ctx, teamID); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}

	s.hub.Broadcast(team.ProjectID, "team.dissolved", map[string]string{"teamId": teamID})
	slog.Info("team dissolved", "team_id", teamID, "team_name", team.Name)
	return nil
}

// DissolveTeamKeepChannel stops all non-lead worker sessions, removes their
// worktrees and branches, deletes them from DB, but keeps the team record and
// the lead session linked. The channel persists as an archived read-only view.
func (s *Service) DissolveTeamKeepChannel(ctx context.Context, teamID string) error {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	project, projErr := s.queries.GetProject(ctx, team.ProjectID)

	for _, m := range members {
		if m.TeamRole == "lead" {
			// Keep lead linked — just clear callbacks.
			if live := s.mgr.Get(m.ID); live != nil {
				live.SetAgentMessageCallback(nil)
			}
			continue
		}

		// Stop worker CLI process.
		if live := s.mgr.Get(m.ID); live != nil {
			live.SetAgentMessageCallback(nil)
			_ = s.mgr.Stop(ctx, m.ID)
		}

		// Remove worktree and force-delete branch.
		if projErr == nil {
			if wtPath := nullStr(m.WorktreePath); wtPath != "" {
				gitops.RemoveWorktree(project.Path, wtPath)
			}
			if branch := nullStr(m.WorktreeBranch); branch != "" {
				if delErr := gitops.ForceDeleteBranch(project.Path, branch); delErr != nil {
					slog.Warn("dissolve-keep: branch force-delete failed",
						"session_id", m.ID, "branch", branch, "error", delErr)
				}
				gitops.DeleteRemoteBranch(project.Path, branch)
			}
		}

		// Delete session from DB.
		if err := s.queries.DeleteSession(ctx, m.ID); err != nil {
			slog.Warn("dissolve-keep: session delete failed", "session_id", m.ID, "error", err)
			continue
		}
		if s.gitSvc != nil {
			s.gitSvc.CleanupVersion(m.ID)
		}
		s.hub.Broadcast(team.ProjectID, "session.deleted", map[string]any{
			"sessionId": m.ID,
		})
	}

	// Broadcast updated team (workers removed, team still exists).
	info, err := s.buildTeamInfo(ctx, team)
	if err != nil {
		return fmt.Errorf("build team info: %w", err)
	}
	s.hub.Broadcast(team.ProjectID, "team.updated", info)
	slog.Info("team dissolved (keep channel)", "team_id", teamID, "team_name", team.Name)
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
	// Defensive: verify the just-joined session appears in the member list.
	// With SQLite connection pool + WAL, a read on a different connection may
	// miss a just-committed write. Retry once if the joiner is absent.
	if buildErr == nil {
		found := false
		for _, m := range info.Members {
			if m.SessionID == sessionID {
				found = true
				break
			}
		}
		if !found {
			info, buildErr = s.buildTeamInfo(ctx, team)
		}
	}
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

	// Wire callbacks and inject team context into a live session.
	if live := s.mgr.Get(sessionID); live != nil {
		s.wireAgentMessageCallback(live, teamID)
		if role == "lead" {
			s.wireDissolveTeamCallback(live, teamID)
		}
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

// persistAgentMessage persists an agent message event on a session and broadcasts it.
func (s *Service) persistAgentMessage(ctx context.Context, sessionID, projectID string, event WireAgentMessageEvent) {
	live := s.mgr.Get(sessionID)
	turnIndex := int64(0)
	seq := int64(0)
	if live != nil {
		t, sq := live.pipeline.AllocSeq()
		turnIndex = int64(t)
		seq = int64(sq)
	}
	eventData, _ := json.Marshal(event)
	if err := s.queries.InsertEvent(ctx, store.InsertEventParams{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Seq:       seq,
		Type:      "agent_message",
		Data:      string(eventData),
	}); err != nil {
		slog.Warn("persist agent message failed", "session_id", sessionID, "error", err)
	}
	s.hub.Broadcast(projectID, "session.event", map[string]any{
		"sessionId": sessionID,
		"event":     event,
	})
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

	base := WireAgentMessageEvent{
		Type:            "agent_message",
		SenderSessionID: p.SenderSessionID,
		SenderName:      senderSess.Name,
		TargetSessionID: p.TargetSessionID,
		TargetName:      targetSess.Name,
		Content:         p.Content,
	}

	// Persist outgoing copy on sender.
	sentEvent := base
	sentEvent.Direction = DirectionSent
	s.persistAgentMessage(ctx, p.SenderSessionID, senderSess.ProjectID, sentEvent)

	// Persist incoming copy on target.
	recvEvent := base
	recvEvent.Direction = DirectionReceived
	s.persistAgentMessage(ctx, p.TargetSessionID, targetSess.ProjectID, recvEvent)

	// Deliver to the target's CLI via SendMessage.
	if live := s.mgr.Get(p.TargetSessionID); live != nil {
		formatted := claudecli.FormatAgentMessage(senderSess.Name, p.Content)
		if err := live.cliSess.SendMessage(formatted); err != nil {
			slog.Warn("agent message CLI delivery failed", "target", p.TargetSessionID, "error", err)
		}
	}

	return nil
}

// BroadcastToTeam sends a user-authored message to every member of a team.
func (s *Service) BroadcastToTeam(ctx context.Context, teamID, content string) error {
	team, err := s.queries.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	for _, m := range members {
		event := WireAgentMessageEvent{
			Type:     "agent_message",
			FromUser: true,
			Content:  content,
		}
		s.persistAgentMessage(ctx, m.ID, team.ProjectID, event)

		if live := s.mgr.Get(m.ID); live != nil {
			if err := live.cliSess.SendMessage(content); err != nil {
				slog.Warn("broadcast CLI delivery failed", "session_id", m.ID, "error", err)
			}
		}
	}

	return nil
}

// GetTeamTimeline returns all agent messages across team members.
// Deduplicates agent-to-agent messages by keeping only the "sent" copy.
// User broadcast messages (FromUser=true) are always included.
func (s *Service) GetTeamTimeline(ctx context.Context, teamID string) ([]WireAgentMessageEvent, error) {
	events, err := s.queries.ListAgentMessagesByTeam(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list agent messages: %w", err)
	}

	seen := make(map[string]bool)
	messages := make([]WireAgentMessageEvent, 0, len(events)/2+1)
	for _, e := range events {
		var msg WireAgentMessageEvent
		if err := json.Unmarshal([]byte(e.Data), &msg); err != nil {
			continue
		}
		if msg.FromUser {
			// Deduplicate user broadcasts (one per member) by content+session.
			key := "user:" + msg.Content
			if seen[key] {
				continue
			}
			seen[key] = true
			messages = append(messages, msg)
		} else if msg.Direction == DirectionSent {
			messages = append(messages, msg)
		}
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

// SwarmMemberSpec describes a single worker to create in a swarm.
type SwarmMemberSpec struct {
	Name            string          `json:"name"`
	Prompt          string          `json:"prompt"`
	Role            string          `json:"role"`
	Model           string          `json:"model"`
	PlanMode        bool            `json:"planMode"`
	AutoApproveMode string          `json:"autoApproveMode"`
	Effort          string          `json:"effort"`
	BehaviorPresets BehaviorPresets `json:"behaviorPresets"`
}

// CreateSwarmParams holds the parameters for creating a team with multiple sessions.
type CreateSwarmParams struct {
	ProjectID     string
	TeamName      string
	LeadSessionID string // existing session to join as lead (optional)
	Members       []SwarmMemberSpec
}

// CreateSwarmResult is the wire type returned after swarm creation.
type CreateSwarmResult struct {
	TeamID     string   `json:"teamId"`
	SessionIDs []string `json:"sessionIds"`
	Errors     []string `json:"errors,omitempty"`
}

// buildWorkerPrompt wraps a raw worker prompt with team framing so the worker
// knows its role, who the lead is, and that it should report back.
func buildWorkerPrompt(teamName, workerRole, leadName string, peerNames []string, rawPrompt string) string {
	role := workerRole
	if role == "" {
		role = "worker"
	}
	header := fmt.Sprintf(
		"You are a %s on team %q, led by %q.",
		role, teamName, leadName,
	)
	if len(peerNames) > 0 {
		header += fmt.Sprintf(" Your teammates: %s.", strings.Join(peerNames, ", "))
	}
	header += " Complete the task below, commit your changes, " +
		"then message the lead with a summary of what you did and any decisions you made."
	return header + "\n\n## Task\n\n" + rawPrompt
}

// CreateSwarm creates a team and N worker sessions in one operation.
// The lead session (if provided) joins as "lead". Each member gets its own
// worktree and immediately receives the first query. Supports partial success.
func (s *Service) CreateSwarm(ctx context.Context, p CreateSwarmParams) (CreateSwarmResult, error) {
	slog.Info("swarm: creating",
		"team_name", p.TeamName,
		"lead_id", p.LeadSessionID,
		"worker_count", len(p.Members),
	)

	// 1. Create the team.
	team, err := s.CreateTeam(ctx, p.ProjectID, p.TeamName)
	if err != nil {
		return CreateSwarmResult{}, fmt.Errorf("create team: %w", err)
	}

	// 2. Join the lead session if specified.
	var leadName string
	if p.LeadSessionID != "" {
		if _, err := s.JoinTeam(ctx, p.LeadSessionID, team.ID, "lead"); err != nil {
			slog.Warn("swarm: lead join failed", "session_id", p.LeadSessionID, "error", err)
		}
		if dbLead, err := s.queries.GetSession(ctx, p.LeadSessionID); err == nil {
			leadName = dbLead.Name
		}
	}

	// 3. Create each worker session, join team, submit query.
	sessionIDs := make([]string, len(p.Members))
	var errs []string
	for i, member := range p.Members {
		role := member.Role
		if role == "" {
			role = "worker"
		}

		result, err := s.CreateSession(ctx, CreateSessionParams{
			ProjectID:       p.ProjectID,
			Name:            member.Name,
			Worktree:        true,
			Model:           member.Model,
			PlanMode:        member.PlanMode,
			AutoApproveMode: member.AutoApproveMode,
			Effort:          member.Effort,
			BehaviorPresets: member.BehaviorPresets,
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("member %d (%s): %v", i, member.Name, err))
			continue
		}

		sessionIDs[i] = result.SessionID
		slog.Info("swarm: worker created",
			"team_id", team.ID,
			"worker_name", member.Name,
			"worker_role", role,
			"session_id", result.SessionID,
			"auto_approve", member.AutoApproveMode,
		)

		if _, err := s.JoinTeam(ctx, result.SessionID, team.ID, role); err != nil {
			errs = append(errs, fmt.Sprintf("member %d join: %v", i, err))
		}

		// Augment the worker's initial prompt with team framing.
		workerPrompt := member.Prompt
		if leadName != "" {
			// Collect peer names (other workers, not self).
			var peers []string
			for j, other := range p.Members {
				if j != i {
					peers = append(peers, other.Name)
				}
			}
			workerPrompt = buildWorkerPrompt(p.TeamName, member.Role, leadName, peers, member.Prompt)
		}

		if err := s.QuerySession(ctx, result.SessionID, workerPrompt, nil); err != nil {
			errs = append(errs, fmt.Sprintf("member %d query: %v", i, err))
		}
	}

	// 4. Re-inject team context to all live members so everyone sees the full roster.
	s.refreshTeamContext(ctx, team.ID)

	out := CreateSwarmResult{TeamID: team.ID, SessionIDs: sessionIDs}
	if len(errs) > 0 {
		out.Errors = errs
	}
	return out, nil
}

// refreshTeamContext re-injects team preamble to all live members of a team.
func (s *Service) refreshTeamContext(ctx context.Context, teamID string) {
	members, err := s.queries.ListTeamMembers(ctx, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		return
	}
	for _, m := range members {
		if live := s.mgr.Get(m.ID); live != nil {
			go s.injectTeamContext(context.Background(), live, teamID)
		}
	}
}

// wireSpawnWorkersCallback sets up the SpawnWorkers interception callback on a
// live session. On approval, it creates a swarm with the session as lead.
func (s *Service) wireSpawnWorkersCallback(sess *Session, projectID string) {
	sess.SetSpawnWorkersCallback(func(senderID string, req SpawnWorkersRequest) error {
		// Look up current team — if the session is already in one, add workers there.
		// Otherwise, create a new team.
		dbSess, err := s.queries.GetSession(context.Background(), senderID)
		if err != nil {
			return fmt.Errorf("sender not found: %w", err)
		}

		teamName := req.TeamName
		if teamName == "" {
			teamName = dbSess.Name + " workers"
		}

		// Inherit the lead's auto-approve mode and behavior presets so workers
		// don't need manual approval for every tool call.
		leadAutoApprove := dbSess.AutoApproveMode
		leadPresets := ParsePresets(dbSess.BehaviorPresets)
		// Workers always get auto-commit since they're in worktrees.
		leadPresets.AutoCommit = true

		members := make([]SwarmMemberSpec, len(req.Workers))
		for i, w := range req.Workers {
			members[i] = SwarmMemberSpec{
				Name:            w.Name,
				Role:            w.Role,
				Prompt:          w.Prompt,
				AutoApproveMode: leadAutoApprove,
				BehaviorPresets: leadPresets,
			}
		}

		_, err = s.CreateSwarm(context.Background(), CreateSwarmParams{
			ProjectID:     projectID,
			TeamName:      teamName,
			LeadSessionID: senderID,
			Members:       members,
		})
		return err
	})
}

// wireDissolveTeamCallback sets up the @dissolve interception callback on a
// live session. When the leader calls SendMessage(to="@dissolve"), it triggers
// DissolveTeam which cleans up all workers and the team.
func (s *Service) wireDissolveTeamCallback(sess *Session, teamID string) {
	sess.SetDissolveTeamCallback(func(senderID string) error {
		return s.DissolveTeam(context.Background(), teamID)
	})
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
