package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/allbin/agentique/backend/internal/store"
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

// --- End-to-end pipeline → routing tests ---

// TestTeam_E2E_PipelineRoutesMessage verifies the full production path:
// session sends SendMessage ToolUseEvent → pipeline fires OnSendMessage →
// onAgentMessage callback → RouteAgentMessage → events persisted + CLI delivery.
func (s *TeamSuite) TestTeam_E2E_PipelineRoutesMessage() {
	leadID, leadMock := s.createNamedSession("Alice")
	workerID, workerMock := s.createNamedSession("Bob")
	s.createTeamWithMembers("e2e-team", leadID, workerID)

	// Start a query so the event loop is running.
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "do work", nil))

	// Inject a SendMessage ToolUseEvent through the mock CLI — this is what
	// Claude actually produces when it calls SendMessage.
	input, _ := json.Marshal(map[string]string{"to": "Bob", "message": "pipeline e2e test"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_e2e", "SendMessage", json.RawMessage(input))))

	// Give the async pipeline goroutine time to process.
	time.Sleep(200 * time.Millisecond)

	// Complete the turn.
	s.Require().NoError(leadMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), lead, StateIdle)

	// Verify: events persisted on both sessions.
	senderMsgs := s.findAgentMessages(leadID)
	s.Require().NotEmpty(senderMsgs, "sender should have a 'sent' agent_message event")
	s.Equal("pipeline e2e test", senderMsgs[0].Content)

	targetMsgs := s.findAgentMessages(workerID)
	s.Require().NotEmpty(targetMsgs, "target should have a 'received' agent_message event")
	s.Equal("pipeline e2e test", targetMsgs[0].Content)

	// Verify: Bob's CLI mock received the formatted message.
	var delivered bool
	for _, msg := range workerMock.SentMessages() {
		if contains(msg, "pipeline e2e test") {
			delivered = true
			break
		}
	}
	s.True(delivered, "Bob's CLI should have received the message via SendMessage")
}

// TestTeam_E2E_Bidirectional verifies messages route correctly in both
// directions: lead→worker and worker→lead.
func (s *TeamSuite) TestTeam_E2E_Bidirectional() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, workerMock := s.createNamedSession("Worker")
	s.createTeamWithMembers("bidi-team", leadID, workerID)

	// Lead → Worker
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "task 1", nil))

	input, _ := json.Marshal(map[string]string{"to": "Worker", "message": "lead says hi"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_l2w", "SendMessage", json.RawMessage(input))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(leadMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), lead, StateIdle)

	// Worker → Lead
	worker := s.mgr.Get(workerID)
	s.Require().NoError(worker.Query(context.Background(), "task 2", nil))

	input2, _ := json.Marshal(map[string]string{"to": "Lead", "message": "worker says hi"})
	s.Require().NoError(workerMock.Inject(testutil.ToolUseEvent("tu_w2l", "SendMessage", json.RawMessage(input2))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(workerMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), worker, StateIdle)

	// Verify: Lead has 1 sent + 1 received message.
	leadMsgs := s.findAgentMessages(leadID)
	s.Require().Len(leadMsgs, 2, "lead should have sent + received messages")
	directions := map[string]bool{}
	for _, m := range leadMsgs {
		directions[m.Direction] = true
	}
	s.True(directions[DirectionSent], "lead should have a 'sent' message")
	s.True(directions[DirectionReceived], "lead should have a 'received' message")

	// Verify: Worker has 1 received + 1 sent message.
	workerMsgs := s.findAgentMessages(workerID)
	s.Require().Len(workerMsgs, 2, "worker should have received + sent messages")

	// Verify: Lead's CLI received worker's message.
	var leadGotMsg bool
	for _, msg := range leadMock.SentMessages() {
		if contains(msg, "worker says hi") {
			leadGotMsg = true
			break
		}
	}
	s.True(leadGotMsg, "lead CLI should have received worker's message")
}

// TestTeam_LeaveTeam_StopsRouting verifies that after a session leaves a team,
// SendMessage tool calls from that session are no longer routed to teammates.
func (s *TeamSuite) TestTeam_LeaveTeam_StopsRouting() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createTeamWithMembers("leave-team", leadID, workerID)

	// Verify routing works before leaving.
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "before leave",
	})
	s.Require().NoError(err)
	s.Require().Len(s.findAgentMessages(workerID), 1)

	// Leave the team.
	s.Require().NoError(s.svc.LeaveTeam(context.Background(), leadID))

	// Inject a SendMessage ToolUseEvent — it should not route anywhere.
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "more work", nil))

	input, _ := json.Marshal(map[string]string{"to": "Worker", "message": "after leave"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_left", "SendMessage", json.RawMessage(input))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(leadMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), lead, StateIdle)

	// Worker should still only have the 1 message from before leaving.
	workerMsgs := s.findAgentMessages(workerID)
	s.Len(workerMsgs, 1, "no new messages should arrive after leaving team")
	s.Equal("before leave", workerMsgs[0].Content)
}

// TestTeam_Resume_RewireCallback verifies that when a session in a team is
// stopped and resumed, the agent message callback is re-wired and messages
// route correctly again.
func (s *TeamSuite) TestTeam_Resume_RewireCallback() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createTeamWithMembers("resume-team", leadID, workerID)

	// Stop the lead session.
	s.Require().NoError(s.mgr.Stop(context.Background(), leadID))
	s.False(s.mgr.IsLive(leadID))

	// Give the session a Claude session ID so it can be resumed.
	s.Require().NoError(s.Queries.UpdateClaudeSessionID(context.Background(),
		store.UpdateClaudeSessionIDParams{
			ClaudeSessionID: sqlNullString("claude-resume-test"),
			ID:              leadID,
		}))

	// Resume by querying — triggers lazy resume via svc.QuerySession.
	s.Require().NoError(s.svc.QuerySession(context.Background(), leadID, "resumed query", nil))
	s.True(s.mgr.IsLive(leadID))

	// The resumed session should have its callback re-wired.
	// Inject a SendMessage ToolUseEvent through the new mock CLI.
	resumedMock := s.Connector.Last()
	input, _ := json.Marshal(map[string]string{"to": "Worker", "message": "after resume"})
	s.Require().NoError(resumedMock.Inject(testutil.ToolUseEvent("tu_resume", "SendMessage", json.RawMessage(input))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(resumedMock.Inject(testutil.ResultEvent(0.01)))

	lead := s.mgr.Get(leadID)
	waitForState(s.T(), lead, StateIdle)

	// Verify: Worker received the message from the resumed session.
	workerMsgs := s.findAgentMessages(workerID)
	var found bool
	for _, m := range workerMsgs {
		if m.Content == "after resume" {
			found = true
			break
		}
	}
	s.True(found, "worker should receive message from resumed lead session")
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
