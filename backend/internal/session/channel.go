package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// ChannelInfo is the wire type for channel metadata sent to clients.
type ChannelInfo struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Name      string          `json:"name"`
	Members   []ChannelMember `json:"members"`
	CreatedAt string          `json:"createdAt"`
}

// ChannelMember is a lightweight member summary.
type ChannelMember struct {
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
	ChannelID       string `json:"channelId,omitempty"`
	Content         string `json:"content"`
	MessageType     string `json:"messageType,omitempty"`
}

// CreateChannel creates a channel and broadcasts the creation.
func (s *Service) CreateChannel(ctx context.Context, projectID, name string) (ChannelInfo, error) {
	channelID := uuid.New().String()
	ch, err := s.queries.CreateChannel(ctx, store.CreateChannelParams{
		ID:        channelID,
		Name:      name,
		ProjectID: projectID,
	})
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("create channel: %w", err)
	}

	info := ChannelInfo{
		ID:        ch.ID,
		ProjectID: ch.ProjectID,
		Name:      ch.Name,
		Members:   []ChannelMember{},
		CreatedAt: ch.CreatedAt,
	}
	s.hub.Broadcast(projectID, "channel.created", info)
	return info, nil
}

// DeleteChannel removes a channel, clears callbacks on live sessions, and unlinks all members.
// Does NOT clean up worker sessions/worktrees/branches — use DissolveChannel for that.
func (s *Service) DeleteChannel(ctx context.Context, channelID string) error {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	members, _ := s.queries.ListChannelMemberSessions(ctx, channelID)
	for _, m := range members {
		if live := s.mgr.Get(m.ID); live != nil {
			live.RemoveAgentMessageCallback(channelID)
		}
	}
	// ON DELETE CASCADE on channel_members handles cleanup.

	if err := s.queries.DeleteChannel(ctx, channelID); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}

	s.hub.Broadcast(ch.ProjectID, "channel.deleted", map[string]string{"channelId": channelID})
	return nil
}

// DissolveChannel stops all non-lead worker sessions, removes their worktrees and
// branches (force-delete), deletes them from DB, unlinks the leader, and deletes
// the channel. The leader session stays alive as a normal session.
func (s *Service) DissolveChannel(ctx context.Context, channelID string) error {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	project, projErr := s.queries.GetProject(ctx, ch.ProjectID)

	s.dissolveWorkers(ctx, channelID, ch.ProjectID, members, project, projErr == nil,
		func(m store.ListChannelMemberSessionsRow) {
			_ = s.queries.RemoveChannelMember(ctx, store.RemoveChannelMemberParams{
				ChannelID: channelID, SessionID: m.ID,
			})
		}, "dissolve")

	if err := s.queries.DeleteChannel(ctx, channelID); err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	s.hub.Broadcast(ch.ProjectID, "channel.dissolved", map[string]string{"channelId": channelID})
	slog.Info("channel dissolved", "channel_id", channelID, "channel_name", ch.Name)
	return nil
}

// DissolveChannelKeepHistory stops all non-lead worker sessions, removes their
// worktrees and branches, deletes them from DB, but keeps the channel record and
// the lead session linked. The channel persists as an archived read-only view.
func (s *Service) DissolveChannelKeepHistory(ctx context.Context, channelID string) error {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	project, projErr := s.queries.GetProject(ctx, ch.ProjectID)

	s.dissolveWorkers(ctx, channelID, ch.ProjectID, members, project, projErr == nil,
		func(_ store.ListChannelMemberSessionsRow) {}, "dissolve-keep")

	info, err := s.buildChannelInfo(ctx, ch)
	if err != nil {
		return fmt.Errorf("build channel info: %w", err)
	}
	s.hub.Broadcast(ch.ProjectID, "channel.updated", info)
	slog.Info("channel dissolved (keep history)", "channel_id", channelID, "channel_name", ch.Name)
	return nil
}

// dissolveWorkers handles the shared worker-cleanup loop for channel dissolution.
// For each lead member, calls leadFn (unlink or no-op). For each sole-channel
// worker, stops, cleans worktree/branch, and deletes the session.
func (s *Service) dissolveWorkers(
	ctx context.Context,
	channelID, projectID string,
	members []store.ListChannelMemberSessionsRow,
	project store.Project,
	projOK bool,
	leadFn func(m store.ListChannelMemberSessionsRow),
	logPrefix string,
) {
	for _, m := range members {
		if live := s.mgr.Get(m.ID); live != nil {
			live.RemoveAgentMessageCallback(channelID)
		}

		if m.MemberRole == "lead" {
			leadFn(m)
			continue
		}

		// Multi-channel member: just unlink from this channel.
		otherChannels, _ := s.queries.ListSessionChannels(ctx, m.ID)
		if len(otherChannels) > 1 {
			_ = s.queries.RemoveChannelMember(ctx, store.RemoveChannelMemberParams{
				ChannelID: channelID, SessionID: m.ID,
			})
			continue
		}

		// Sole-channel worker: stop, clean up worktree, delete.
		if live := s.mgr.Get(m.ID); live != nil {
			_ = s.mgr.Stop(ctx, m.ID)
		}
		if projOK {
			if wtPath := nullStr(m.WorktreePath); wtPath != "" {
				s.worktree.RemoveWorktree(project.Path, wtPath)
			}
			if branch := nullStr(m.WorktreeBranch); branch != "" {
				if delErr := s.worktree.ForceDeleteBranch(project.Path, branch); delErr != nil {
					slog.Warn(logPrefix+": branch force-delete failed",
						"session_id", m.ID, "branch", branch, "error", delErr)
				}
				s.worktree.DeleteRemoteBranch(project.Path, branch)
			}
		}
		if err := s.queries.DeleteSession(ctx, m.ID); err != nil {
			slog.Warn(logPrefix+": session delete failed", "session_id", m.ID, "error", err)
			continue
		}
		if s.gitSvc != nil {
			s.gitSvc.CleanupVersion(m.ID)
		}
		s.hub.Broadcast(projectID, "session.deleted", PushSessionDeleted{SessionID: m.ID})
	}
}

// JoinChannel adds a session to a channel, broadcasts the change, and returns the
// updated ChannelInfo so the caller (RPC handler) can forward it to the client.
func (s *Service) JoinChannel(ctx context.Context, sessionID, channelID, role string) (ChannelInfo, error) {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("channel not found: %w", err)
	}

	dbSess, err := s.queries.GetSession(ctx, sessionID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("session not found: %w", err)
	}

	// Reject duplicate names within the channel.
	existingMembers, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("list channel members: %w", err)
	}
	for _, m := range existingMembers {
		if m.Name == dbSess.Name && m.ID != sessionID {
			return ChannelInfo{}, fmt.Errorf("channel member named %q already exists; rename this session first", dbSess.Name)
		}
	}

	if err := s.queries.AddChannelMember(ctx, store.AddChannelMemberParams{
		ChannelID: channelID,
		SessionID: sessionID,
		Role:      role,
	}); err != nil {
		return ChannelInfo{}, fmt.Errorf("add channel member: %w", err)
	}

	member := ChannelMember{
		SessionID:    sessionID,
		Name:         dbSess.Name,
		Role:         role,
		State:        dbSess.State,
		Connected:    s.mgr.IsLive(sessionID),
		WorktreePath: nullStr(dbSess.WorktreePath),
	}

	info, buildErr := s.buildChannelInfo(ctx, ch)
	// Defensive: verify the just-joined session appears in the member list.
	if buildErr == nil {
		found := false
		for _, m := range info.Members {
			if m.SessionID == sessionID {
				found = true
				break
			}
		}
		if !found {
			info, buildErr = s.buildChannelInfo(ctx, ch)
		}
	}
	payload := PushChannelMemberJoined{ChannelID: channelID, Member: member}
	if buildErr == nil {
		payload.Channel = &info
	} else {
		slog.Warn("buildChannelInfo after join failed", "channelId", channelID, "error", buildErr)
	}
	s.hub.Broadcast(ch.ProjectID, "channel.member-joined", payload)

	// Wire callbacks for the joining session.
	if live := s.mgr.Get(sessionID); live != nil {
		s.wireAgentMessageCallback(live, channelID)
		if role == "lead" {
			s.wireDissolveChannelCallback(live, channelID)
		}
	}

	// Re-inject channel context to ALL live members so everyone sees the updated roster.
	s.refreshChannelContext(ctx, channelID)

	return info, buildErr
}

// LeaveChannel removes a session from a specific channel.
func (s *Service) LeaveChannel(ctx context.Context, sessionID, channelID string) error {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	if err := s.queries.RemoveChannelMember(ctx, store.RemoveChannelMemberParams{
		ChannelID: channelID,
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("remove channel member: %w", err)
	}

	// Clear agent message callback for this channel.
	if live := s.mgr.Get(sessionID); live != nil {
		live.RemoveAgentMessageCallback(channelID)
	}

	s.hub.Broadcast(ch.ProjectID, "channel.member-left", PushChannelMemberLeft{ChannelID: channelID, SessionID: sessionID})
	return nil
}

// GetChannelInfo returns channel metadata with members.
func (s *Service) GetChannelInfo(ctx context.Context, channelID string) (ChannelInfo, error) {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("channel not found: %w", err)
	}
	return s.buildChannelInfo(ctx, ch)
}

// ListChannels returns all channels for a project.
func (s *Service) ListChannels(ctx context.Context, projectID string) ([]ChannelInfo, error) {
	channels, err := s.queries.ListChannelsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}

	infos := make([]ChannelInfo, 0, len(channels))
	for _, ch := range channels {
		info, err := s.buildChannelInfo(ctx, ch)
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
	s.hub.Broadcast(projectID, "session.event", PushSessionEvent{SessionID: sessionID, Event: event})
}

// RouteAgentMessage delivers a message from one session to another within the same channel.
func (s *Service) RouteAgentMessage(ctx context.Context, p AgentMessagePayload) error {
	senderSess, err := s.queries.GetSession(ctx, p.SenderSessionID)
	if err != nil {
		return fmt.Errorf("sender not found: %w", err)
	}
	targetSess, err := s.queries.GetSession(ctx, p.TargetSessionID)
	if err != nil {
		return fmt.Errorf("target not found: %w", err)
	}

	// Find a shared channel. If p.ChannelID is set, verify both are members.
	channelID := p.ChannelID
	if channelID == "" {
		channelID, err = s.findSharedChannel(ctx, p.SenderSessionID, p.TargetSessionID)
		if err != nil {
			return err
		}
	} else {
		// Verify both sessions are members of the specified channel.
		senderChannels, _ := s.queries.ListSessionChannels(ctx, p.SenderSessionID)
		targetChannels, _ := s.queries.ListSessionChannels(ctx, p.TargetSessionID)
		senderIn := false
		targetIn := false
		for _, c := range senderChannels {
			if c.ChannelID == channelID {
				senderIn = true
			}
		}
		for _, c := range targetChannels {
			if c.ChannelID == channelID {
				targetIn = true
			}
		}
		if !senderIn || !targetIn {
			return fmt.Errorf("sender and target must be in the same channel")
		}
	}

	base := WireAgentMessageEvent{
		Type:            "agent_message",
		ChannelID:       channelID,
		SenderSessionID: p.SenderSessionID,
		SenderName:      senderSess.Name,
		TargetSessionID: p.TargetSessionID,
		TargetName:      targetSess.Name,
		Content:         p.Content,
		MessageType:     p.MessageType,
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
		// If the target is idle, transition to running so the UI reflects
		// activity. OnTurnComplete will transition back when Claude finishes.
		// Skip if already running — SendMessage just appends to the current turn.
		if live.State() == StateIdle {
			if err := live.setState(StateRunning); err != nil {
				slog.Warn("agent message state transition failed", "target", p.TargetSessionID, "error", err)
			}
		}

		formatted := formatAgentMessageWithType(senderSess.Name, p.Content, p.MessageType)
		if err := live.cliSess.SendMessage(formatted); err != nil {
			slog.Warn("agent message CLI delivery failed", "target", p.TargetSessionID, "error", err)
			// Best-effort revert: if the CLI rejected the message and the session
			// was idle before, try to restore idle so it's not stuck in running.
			if live.State() == StateRunning {
				_ = live.setState(StateIdle)
			}
		}
	}

	return nil
}

// findSharedChannel returns a channel that both sessions belong to.
func (s *Service) findSharedChannel(ctx context.Context, sessionA, sessionB string) (string, error) {
	aChannels, err := s.queries.ListSessionChannels(ctx, sessionA)
	if err != nil {
		return "", fmt.Errorf("list sender channels: %w", err)
	}
	bChannels, err := s.queries.ListSessionChannels(ctx, sessionB)
	if err != nil {
		return "", fmt.Errorf("list target channels: %w", err)
	}
	bSet := make(map[string]bool, len(bChannels))
	for _, c := range bChannels {
		bSet[c.ChannelID] = true
	}
	for _, c := range aChannels {
		if bSet[c.ChannelID] {
			return c.ChannelID, nil
		}
	}
	return "", fmt.Errorf("sender and target must be in the same channel")
}

// BroadcastToChannel sends a user-authored message to every member of a channel.
func (s *Service) BroadcastToChannel(ctx context.Context, channelID, content string) error {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	for _, m := range members {
		event := WireAgentMessageEvent{
			Type:      "agent_message",
			ChannelID: channelID,
			FromUser:  true,
			Content:   content,
		}
		s.persistAgentMessage(ctx, m.ID, ch.ProjectID, event)

		if live := s.mgr.Get(m.ID); live != nil {
			if err := live.cliSess.SendMessage(content); err != nil {
				slog.Warn("broadcast CLI delivery failed", "session_id", m.ID, "error", err)
			}
		}
	}

	return nil
}

// GetChannelTimeline returns all agent messages across channel members.
// Deduplicates agent-to-agent messages by keeping only the "sent" copy.
// User broadcast messages (FromUser=true) are always included.
func (s *Service) GetChannelTimeline(ctx context.Context, channelID string) ([]WireAgentMessageEvent, error) {
	events, err := s.queries.ListAgentMessagesByChannel(ctx, channelID)
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

func (s *Service) buildChannelInfo(ctx context.Context, ch store.Channel) (ChannelInfo, error) {
	members, err := s.queries.ListChannelMemberSessions(ctx, ch.ID)
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("list members: %w", err)
	}

	memberInfos := make([]ChannelMember, 0, len(members))
	for _, m := range members {
		memberInfos = append(memberInfos, ChannelMember{
			SessionID:    m.ID,
			Name:         m.Name,
			Role:         m.MemberRole,
			State:        m.State,
			Connected:    s.mgr.IsLive(m.ID),
			WorktreePath: nullStr(m.WorktreePath),
		})
	}

	return ChannelInfo{
		ID:        ch.ID,
		ProjectID: ch.ProjectID,
		Name:      ch.Name,
		Members:   memberInfos,
		CreatedAt: ch.CreatedAt,
	}, nil
}

// buildChannelPreamble creates channel context for the system prompt, excluding the given session.
func (s *Service) buildChannelPreamble(ctx context.Context, channelID, excludeSessionID string) *ChannelPreambleInfo {
	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return nil
	}
	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return nil
	}
	var peers []ChannelPreambleMember
	for _, m := range members {
		if m.ID == excludeSessionID {
			continue
		}
		peers = append(peers, ChannelPreambleMember{
			Name:         m.Name,
			Role:         m.MemberRole,
			WorktreePath: nullStr(m.WorktreePath),
		})
	}
	if len(peers) == 0 {
		return nil
	}
	return &ChannelPreambleInfo{
		ChannelName: ch.Name,
		Members:     peers,
	}
}

// buildAllChannelPreambles builds preamble info for all channels a session belongs to.
func (s *Service) buildAllChannelPreambles(ctx context.Context, sessionID string) []*ChannelPreambleInfo {
	memberships, err := s.queries.ListSessionChannels(ctx, sessionID)
	if err != nil {
		return nil
	}
	var result []*ChannelPreambleInfo
	for _, cm := range memberships {
		info := s.buildChannelPreamble(ctx, cm.ChannelID, sessionID)
		if info != nil {
			result = append(result, info)
		}
	}
	return result
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

// CreateSwarmParams holds the parameters for creating a channel with multiple sessions.
type CreateSwarmParams struct {
	ProjectID     string
	ChannelName   string
	LeadSessionID string // existing session to join as lead (optional)
	Members       []SwarmMemberSpec
}

// CreateSwarmResult is the wire type returned after swarm creation.
type CreateSwarmResult struct {
	ChannelID  string   `json:"channelId"`
	SessionIDs []string `json:"sessionIds"`
	Errors     []string `json:"errors,omitempty"`
}

// buildWorkerPrompt wraps a raw worker prompt with channel framing so the worker
// knows its role, who the lead is, and that it should report back.
func buildWorkerPrompt(channelName, workerRole, leadName string, peerNames []string, rawPrompt string) string {
	role := workerRole
	if role == "" {
		role = "worker"
	}
	header := fmt.Sprintf(
		"You are a %s on channel %q, led by %q.",
		role, channelName, leadName,
	)
	if len(peerNames) > 0 {
		header += fmt.Sprintf(" Your teammates: %s.", strings.Join(peerNames, ", "))
	}
	header += "\n\n## Communication Protocol\n\n" +
		"Always include a `type` field in SendMessage to signal your status:\n\n" +
		"1. **Before starting:** `SendMessage({to: \"" + leadName + "\", message: \"...\", type: \"plan\"})`\n" +
		"2. **After each commit:** `SendMessage({to: \"" + leadName + "\", message: \"...\", type: \"progress\"})`\n" +
		"3. **When finished:** `SendMessage({to: \"" + leadName + "\", message: \"...\", type: \"done\"})`"
	return header + "\n\n## Task\n\n" + rawPrompt
}

// CreateSwarm creates a channel and N worker sessions in one operation.
// The lead session (if provided) joins as "lead". Each member gets its own
// worktree and immediately receives the first query. Supports partial success.
func (s *Service) CreateSwarm(ctx context.Context, p CreateSwarmParams) (CreateSwarmResult, error) {
	slog.Info("swarm: creating",
		"channel_name", p.ChannelName,
		"lead_id", p.LeadSessionID,
		"worker_count", len(p.Members),
	)

	// 1. Create the channel.
	ch, err := s.CreateChannel(ctx, p.ProjectID, p.ChannelName)
	if err != nil {
		return CreateSwarmResult{}, fmt.Errorf("create channel: %w", err)
	}

	// 2. Join the lead session if specified.
	var leadName string
	if p.LeadSessionID != "" {
		if _, err := s.JoinChannel(ctx, p.LeadSessionID, ch.ID, "lead"); err != nil {
			slog.Warn("swarm: lead join failed", "session_id", p.LeadSessionID, "error", err)
		}
		if dbLead, err := s.queries.GetSession(ctx, p.LeadSessionID); err == nil {
			leadName = dbLead.Name
		}
	}

	// 3. Create each worker session, join channel, submit query.
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
			"channel_id", ch.ID,
			"worker_name", member.Name,
			"worker_role", role,
			"session_id", result.SessionID,
			"auto_approve", member.AutoApproveMode,
		)

		if _, err := s.JoinChannel(ctx, result.SessionID, ch.ID, role); err != nil {
			errs = append(errs, fmt.Sprintf("member %d join: %v", i, err))
		}

		// Augment the worker's initial prompt with channel framing.
		workerPrompt := member.Prompt
		if leadName != "" {
			var peers []string
			for j, other := range p.Members {
				if j != i {
					peers = append(peers, other.Name)
				}
			}
			workerPrompt = buildWorkerPrompt(p.ChannelName, member.Role, leadName, peers, member.Prompt)
		}

		if err := s.QuerySession(ctx, result.SessionID, workerPrompt, nil); err != nil {
			errs = append(errs, fmt.Sprintf("member %d query: %v", i, err))
		}
	}

	// 4. Re-inject channel context to all live members so everyone sees the full roster.
	s.refreshChannelContext(ctx, ch.ID)

	out := CreateSwarmResult{ChannelID: ch.ID, SessionIDs: sessionIDs}
	if len(errs) > 0 {
		out.Errors = errs
	}
	return out, nil
}

// refreshChannelContext re-injects channel preamble to all live members of a channel.
func (s *Service) refreshChannelContext(ctx context.Context, channelID string) {
	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return
	}
	for _, m := range members {
		if live := s.mgr.Get(m.ID); live != nil {
			go s.injectChannelContext(context.Background(), live, channelID)
		}
	}
}

// wireSpawnWorkersCallback sets up the SpawnWorkers interception callback on a
// live session. On approval, it creates a swarm with the session as lead.
func (s *Service) wireSpawnWorkersCallback(sess *Session, projectID string) {
	sess.SetSpawnWorkersCallback(func(senderID string, req SpawnWorkersRequest) error {
		// Look up current channels — if the session is already in one, add workers there.
		// Otherwise, create a new channel.
		channelName := req.ChannelName
		if channelName == "" {
			dbSess, err := s.queries.GetSession(context.Background(), senderID)
			if err != nil {
				return fmt.Errorf("sender not found: %w", err)
			}
			channelName = dbSess.Name + " workers"
		}

		dbSess, err := s.queries.GetSession(context.Background(), senderID)
		if err != nil {
			return fmt.Errorf("sender not found: %w", err)
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
			ChannelName:   channelName,
			LeadSessionID: senderID,
			Members:       members,
		})
		return err
	})
}

// wireDissolveChannelCallback sets up the @dissolve interception callback on a
// live session. When the leader calls SendMessage(to="@dissolve"), it triggers
// DissolveChannel which cleans up all workers and the channel.
func (s *Service) wireDissolveChannelCallback(sess *Session, channelID string) {
	sess.SetDissolveChannelCallback(func(senderID string) error {
		return s.DissolveChannel(context.Background(), channelID)
	})
}

// wireAgentMessageCallback sets up a SendMessage interception callback for a
// specific channel on a live session. The callback resolves the target name to
// a session ID within the channel and routes the message through RouteAgentMessage.
func (s *Service) wireAgentMessageCallback(sess *Session, channelID string) {
	sess.SetAgentMessageCallback(channelID, func(senderID, targetName, content, msgType string) error {
		members, err := s.queries.ListChannelMemberSessions(context.Background(), channelID)
		if err != nil {
			return fmt.Errorf("list channel members: %w", err)
		}
		for _, m := range members {
			if m.Name == targetName {
				return s.RouteAgentMessage(context.Background(), AgentMessagePayload{
					SenderSessionID: senderID,
					TargetSessionID: m.ID,
					ChannelID:       channelID,
					Content:         content,
					MessageType:     msgType,
				})
			}
		}
		return fmt.Errorf("no channel member named %q", targetName)
	})
}

// formatAgentMessageWithType wraps a message with type prefix for CLI delivery.
// For the default "message" type, no prefix is added.
func formatAgentMessageWithType(senderName, content, msgType string) string {
	prefix := ""
	switch msgType {
	case "plan":
		prefix = "[PLAN]\n"
	case "progress":
		prefix = "[PROGRESS]\n"
	case "done":
		prefix = "[DONE]\n"
	}
	return claudecli.FormatAgentMessage(senderName, prefix+content)
}

// injectChannelContext sends a message to a live session about its channel peers.
func (s *Service) injectChannelContext(ctx context.Context, sess *Session, channelID string) {
	members, err := s.queries.ListChannelMemberSessions(ctx, channelID)
	if err != nil {
		return
	}

	ch, err := s.queries.GetChannel(ctx, channelID)
	if err != nil {
		return
	}

	msg := fmt.Sprintf("You have joined channel %q. Your teammates:\n", ch.Name)
	for _, m := range members {
		if m.ID == sess.ID {
			continue
		}
		line := fmt.Sprintf("- %q", m.Name)
		if m.MemberRole != "" {
			line += fmt.Sprintf(" (role: %s)", m.MemberRole)
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
		slog.Warn("channel context injection failed", "session_id", sess.ID, "error", err)
	}
}
