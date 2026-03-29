package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/allbin/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type TeamSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestTeamSuite(t *testing.T) {
	suite.Run(t, new(TeamSuite))
}

func (s *TeamSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// createNamedSession creates a live session and renames it.
func (s *TeamSuite) createNamedSession(name string) (string, *testutil.MockCLISession) {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      name,
		Model:     "opus",
	})
	s.Require().NoError(err)
	mock := s.Connector.Last()
	return result.SessionID, mock
}

// createTeamWithMembers creates a team and joins the given sessions.
func (s *TeamSuite) createTeamWithMembers(teamName string, sessionIDs ...string) string {
	ctx := context.Background()
	team, err := s.svc.CreateTeam(ctx, s.Project.ID, teamName)
	s.Require().NoError(err)
	for _, sid := range sessionIDs {
		_, err := s.svc.JoinTeam(ctx, sid, team.ID, "")
		s.Require().NoError(err)
	}
	return team.ID
}

// findAgentMessages extracts agent_message events from a session's event log.
func (s *TeamSuite) findAgentMessages(sessionID string) []WireAgentMessageEvent {
	events, err := s.Queries.ListEventsBySession(context.Background(), sessionID)
	s.Require().NoError(err)
	var msgs []WireAgentMessageEvent
	for _, e := range events {
		if e.Type == "agent_message" {
			var msg WireAgentMessageEvent
			s.Require().NoError(json.Unmarshal([]byte(e.Data), &msg))
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// --- RouteAgentMessage tests ---

func (s *TeamSuite) TestTeam_RouteMessage_PersistsBothCopies() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createTeamWithMembers("test-team", leadID, workerID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "hello worker",
	})
	s.Require().NoError(err)

	// Sender should have a "sent" copy.
	senderMsgs := s.findAgentMessages(leadID)
	s.Require().Len(senderMsgs, 1)
	s.Equal(DirectionSent, senderMsgs[0].Direction)
	s.Equal("Lead", senderMsgs[0].SenderName)
	s.Equal("Worker", senderMsgs[0].TargetName)
	s.Equal("hello worker", senderMsgs[0].Content)

	// Target should have a "received" copy.
	targetMsgs := s.findAgentMessages(workerID)
	s.Require().Len(targetMsgs, 1)
	s.Equal(DirectionReceived, targetMsgs[0].Direction)
	s.Equal("Lead", targetMsgs[0].SenderName)
	s.Equal("Worker", targetMsgs[0].TargetName)
	s.Equal("hello worker", targetMsgs[0].Content)
}

func (s *TeamSuite) TestTeam_RouteMessage_LiveDelivery() {
	leadID, _ := s.createNamedSession("Sender")
	workerID, workerMock := s.createNamedSession("Target")
	s.createTeamWithMembers("test-team", leadID, workerID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "build the API",
	})
	s.Require().NoError(err)

	// The target's CLI mock should have received a formatted message.
	sent := workerMock.SentMessages()
	s.Require().Len(sent, 1, "expected exactly 1 SendMessage call on target CLI")
	s.Contains(sent[0], "Sender")
	s.Contains(sent[0], "build the API")
}

func (s *TeamSuite) TestTeam_RouteMessage_OfflineTarget() {
	leadID, _ := s.createNamedSession("Lead")
	// Create a DB-only session (not live).
	offlineSess := testutil.SeedSession(s.T(), s.Queries, s.Project.ID, "stopped")
	s.Require().NoError(s.svc.RenameSession(context.Background(), offlineSess.ID, "Offline"))

	s.createTeamWithMembers("test-team", leadID, offlineSess.ID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: offlineSess.ID,
		Content:         "are you there?",
	})
	s.Require().NoError(err)

	// Events should still be persisted on both sides.
	senderMsgs := s.findAgentMessages(leadID)
	s.Require().Len(senderMsgs, 1)
	s.Equal(DirectionSent, senderMsgs[0].Direction)

	targetMsgs := s.findAgentMessages(offlineSess.ID)
	s.Require().Len(targetMsgs, 1)
	s.Equal(DirectionReceived, targetMsgs[0].Direction)

	// No CLI mock exists for offline session — no panic, no error.
}

func (s *TeamSuite) TestTeam_RouteMessage_DifferentTeams() {
	aID, _ := s.createNamedSession("AgentA")
	bID, _ := s.createNamedSession("AgentB")

	s.createTeamWithMembers("team-alpha", aID)
	s.createTeamWithMembers("team-beta", bID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: aID,
		TargetSessionID: bID,
		Content:         "cross-team",
	})
	s.Error(err)
	s.Contains(err.Error(), "same team")
}

func (s *TeamSuite) TestTeam_RouteMessage_NonexistentSession() {
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: "nonexistent-sender",
		TargetSessionID: "nonexistent-target",
		Content:         "hello",
	})
	s.Error(err)
	s.Contains(err.Error(), "not found")
}

func (s *TeamSuite) TestTeam_RouteMessage_BroadcastEvents() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createTeamWithMembers("test-team", leadID, workerID)

	s.Broadcaster.Reset()
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "status update",
	})
	s.Require().NoError(err)

	// Should broadcast session.event for both sender and target.
	msgs := s.Broadcaster.MessagesOfType("session.event")
	s.GreaterOrEqual(len(msgs), 2, "expected at least 2 session.event broadcasts (sent + received)")
}

// --- wireAgentMessageCallback tests ---

func (s *TeamSuite) TestTeam_CallbackRoutesViaName() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, workerMock := s.createNamedSession("Worker")
	teamID := s.createTeamWithMembers("test-team", leadID, workerID)

	// JoinTeam wires the callback. Verify the lead can route by name.
	lead := s.mgr.Get(leadID)
	s.Require().NotNil(lead)

	// The callback was set by JoinTeam → wireAgentMessageCallback.
	lead.mu.Lock()
	cb := lead.onAgentMessage
	lead.mu.Unlock()
	s.Require().NotNil(cb, "onAgentMessage callback should be set after JoinTeam")

	err := cb(leadID, "Worker", "routed via callback")
	s.Require().NoError(err)

	// Verify the message was persisted on both sessions.
	senderMsgs := s.findAgentMessages(leadID)
	s.Require().Len(senderMsgs, 1)
	s.Equal("routed via callback", senderMsgs[0].Content)

	targetMsgs := s.findAgentMessages(workerID)
	s.Require().Len(targetMsgs, 1)
	s.Equal("routed via callback", targetMsgs[0].Content)

	// Verify CLI delivery on the worker's mock.
	sent := workerMock.SentMessages()
	// JoinTeam also injects team context — filter for the routed message.
	var found bool
	for _, msg := range sent {
		if contains(msg, "routed via callback") {
			found = true
			break
		}
	}
	s.True(found, "worker CLI should have received the routed message; got: %v", sent)
	_ = teamID
}

func (s *TeamSuite) TestTeam_CallbackUnknownTarget() {
	leadID, _ := s.createNamedSession("Solo")
	s.createTeamWithMembers("test-team", leadID)

	lead := s.mgr.Get(leadID)
	s.Require().NotNil(lead)

	lead.mu.Lock()
	cb := lead.onAgentMessage
	lead.mu.Unlock()
	s.Require().NotNil(cb)

	err := cb(leadID, "Ghost", "hello?")
	s.Error(err)
	s.Contains(err.Error(), "no team member named")
}

// --- GetTeamTimeline tests ---

func (s *TeamSuite) TestTeam_Timeline_DeduplicatesSent() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	teamID := s.createTeamWithMembers("test-team", leadID, workerID)

	s.Require().NoError(s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "timeline test",
	}))

	timeline, err := s.svc.GetTeamTimeline(context.Background(), teamID)
	s.Require().NoError(err)
	s.Len(timeline, 1, "timeline should deduplicate — only 'sent' copy")
	s.Equal(DirectionSent, timeline[0].Direction)
	s.Equal("timeline test", timeline[0].Content)
}

// --- JoinTeam context injection test ---

func (s *TeamSuite) TestTeam_JoinInjectsContext() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createTeamWithMembers("test-team", leadID, workerID)

	// injectTeamContext runs in a goroutine — give it a moment.
	time.Sleep(100 * time.Millisecond)

	// Both sessions should have received team context via SendMessage.
	leadSent := leadMock.SentMessages()
	var found bool
	for _, msg := range leadSent {
		if contains(msg, "Worker") && contains(msg, "SendMessage") {
			found = true
			break
		}
	}
	s.True(found, "lead should have received team context mentioning Worker")
}

// contains checks if s contains substr (avoids importing strings in test).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
