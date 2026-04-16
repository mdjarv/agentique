package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type ChannelSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestChannelSuite(t *testing.T) {
	suite.Run(t, new(ChannelSuite))
}

func (s *ChannelSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// createNamedSession creates a live session and renames it.
func (s *ChannelSuite) createNamedSession(name string) (string, *testutil.MockCLISession) {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      name,
		Model:     "opus",
	})
	s.Require().NoError(err)
	mock := s.Connector.Last()
	return result.SessionID, mock
}

// createChannelWithMembers creates a channel and joins the given sessions.
func (s *ChannelSuite) createChannelWithMembers(channelName string, sessionIDs ...string) string {
	ctx := context.Background()
	ch, err := s.svc.CreateChannel(ctx, s.Project.ID, channelName)
	s.Require().NoError(err)
	for _, sid := range sessionIDs {
		_, err := s.svc.JoinChannel(ctx, sid, ch.ID, "")
		s.Require().NoError(err)
	}
	return ch.ID
}

// findAgentMessages extracts agent_message events from a session's event log.
func (s *ChannelSuite) findAgentMessages(sessionID string) []WireAgentMessageEvent {
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

func (s *ChannelSuite) TestChannel_RouteMessage_PersistsBothCopies() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createChannelWithMembers("test-team", leadID, workerID)

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

func (s *ChannelSuite) TestChannel_RouteMessage_LiveDelivery() {
	leadID, _ := s.createNamedSession("Sender")
	workerID, workerMock := s.createNamedSession("Target")
	s.createChannelWithMembers("test-team", leadID, workerID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "build the API",
	})
	s.Require().NoError(err)

	// The target's CLI mock should have received at least the routed message.
	// (JoinChannel may also inject channel context via a goroutine.)
	sent := workerMock.SentMessages()
	s.Require().NotEmpty(sent, "expected at least 1 SendMessage call on target CLI")
	last := sent[len(sent)-1]
	s.Contains(last, "Sender")
	s.Contains(last, "build the API")
}

func (s *ChannelSuite) TestChannel_RouteMessage_OfflineTarget() {
	leadID, _ := s.createNamedSession("Lead")
	// Create a DB-only session (not live).
	offlineSess := testutil.SeedSession(s.T(), s.Queries, s.Project.ID, "stopped")
	s.Require().NoError(s.svc.RenameSession(context.Background(), offlineSess.ID, "Offline"))

	s.createChannelWithMembers("test-team", leadID, offlineSess.ID)

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

func (s *ChannelSuite) TestChannel_RouteMessage_DifferentTeams() {
	aID, _ := s.createNamedSession("AgentA")
	bID, _ := s.createNamedSession("AgentB")

	s.createChannelWithMembers("team-alpha", aID)
	s.createChannelWithMembers("team-beta", bID)

	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: aID,
		TargetSessionID: bID,
		Content:         "cross-team",
	})
	s.Error(err)
	s.Contains(err.Error(), "same channel")
}

func (s *ChannelSuite) TestChannel_RouteMessage_NonexistentSession() {
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: "nonexistent-sender",
		TargetSessionID: "nonexistent-target",
		Content:         "hello",
	})
	s.Error(err)
	s.Contains(err.Error(), "not found")
}

func (s *ChannelSuite) TestChannel_RouteMessage_BroadcastEvents() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createChannelWithMembers("test-team", leadID, workerID)

	s.Broadcaster.Reset()
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "status update",
	})
	s.Require().NoError(err)

	// Dual-write broadcasts session.event for both sender and target (legacy),
	// plus a channel.message broadcast for the unified timeline.
	sessionEvts := s.Broadcaster.MessagesOfType("session.event")
	s.GreaterOrEqual(len(sessionEvts), 2, "expected at least 2 session.event broadcasts (sent + received)")
	channelMsgs := s.Broadcaster.MessagesOfType("channel.message")
	s.GreaterOrEqual(len(channelMsgs), 1, "expected at least 1 channel.message broadcast")
}

// --- wireAgentMessageCallback tests ---

func (s *ChannelSuite) TestChannel_CallbackRoutesViaName() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, workerMock := s.createNamedSession("Worker")
	channelID := s.createChannelWithMembers("test-team", leadID, workerID)

	// JoinChannel wires the callback. Verify the lead can route by name.
	lead := s.mgr.Get(leadID)
	s.Require().NotNil(lead)

	// The callback was set by JoinChannel → wireAgentMessageCallback.
	lead.mu.Lock()
	cb := lead.channel.agentMessageCallbacks[channelID]
	lead.mu.Unlock()
	s.Require().NotNil(cb, "agentMessageCallback should be set after JoinChannel")

	err := cb(leadID, "Worker", "routed via callback", "message")
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
	// JoinChannel also injects channel context — filter for the routed message.
	var found bool
	for _, msg := range sent {
		if contains(msg, "routed via callback") {
			found = true
			break
		}
	}
	s.True(found, "worker CLI should have received the routed message; got: %v", sent)
}

func (s *ChannelSuite) TestChannel_CallbackUnknownTarget() {
	leadID, _ := s.createNamedSession("Solo")
	channelID := s.createChannelWithMembers("test-team", leadID)

	lead := s.mgr.Get(leadID)
	s.Require().NotNil(lead)

	lead.mu.Lock()
	cb := lead.channel.agentMessageCallbacks[channelID]
	lead.mu.Unlock()
	s.Require().NotNil(cb)

	err := cb(leadID, "Ghost", "hello?", "message")
	s.Error(err)
	s.Contains(err.Error(), "no channel member named")
}

// --- GetChannelTimeline tests ---

func (s *ChannelSuite) TestChannel_Timeline_SingleEntry() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	channelID := s.createChannelWithMembers("test-team", leadID, workerID)

	s.Require().NoError(s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "timeline test",
	}))

	timeline, err := s.svc.GetChannelTimeline(context.Background(), channelID)
	s.Require().NoError(err)
	s.Len(timeline, 1, "messages table stores one row per message")
	s.Equal("timeline test", timeline[0].Content)
	s.Equal("session", timeline[0].SenderType)
	s.Equal("Lead", timeline[0].SenderName)
}

// --- JoinChannel context injection test ---

func (s *ChannelSuite) TestChannel_JoinInjectsContext() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createChannelWithMembers("test-team", leadID, workerID)

	// injectChannelContext runs in a goroutine — poll until it delivers.
	deadline := time.After(2 * time.Second)
	for {
		leadSent := leadMock.SentMessages()
		for _, msg := range leadSent {
			if contains(msg, "Worker") && contains(msg, "SendMessage") {
				return // success
			}
		}
		select {
		case <-deadline:
			s.Fail("lead should have received channel context mentioning Worker")
			return
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// --- End-to-end pipeline → routing tests ---

// TestChannel_E2E_PipelineRoutesMessage verifies the full production path:
// session sends SendMessage ToolUseEvent → pipeline fires OnSendMessage →
// onAgentMessage callback → RouteAgentMessage → events persisted + CLI delivery.
func (s *ChannelSuite) TestChannel_E2E_PipelineRoutesMessage() {
	leadID, leadMock := s.createNamedSession("Alice")
	workerID, workerMock := s.createNamedSession("Bob")
	s.createChannelWithMembers("e2e-team", leadID, workerID)

	// Start a query so the event loop is running.
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "do work", nil))

	// Inject a SendMessage ToolUseEvent through the mock CLI — this is what
	// Claude actually produces when it calls SendMessage.
	input, _ := json.Marshal(map[string]string{"to": "Bob", "message": "pipeline e2e test"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_e2e", ChannelSendMessageTool, json.RawMessage(input))))

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

// TestChannel_E2E_Bidirectional verifies messages route correctly in both
// directions: lead→worker and worker→lead.
func (s *ChannelSuite) TestChannel_E2E_Bidirectional() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, workerMock := s.createNamedSession("Worker")
	s.createChannelWithMembers("bidi-team", leadID, workerID)

	// Lead → Worker
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "task 1", nil))

	input, _ := json.Marshal(map[string]string{"to": "Worker", "message": "lead says hi"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_l2w", ChannelSendMessageTool, json.RawMessage(input))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(leadMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), lead, StateIdle)

	// The routed message transitioned Worker from idle→running (simulating a new
	// CLI turn). Inject a result event to complete that turn before starting the next.
	worker := s.mgr.Get(workerID)
	s.Require().NoError(workerMock.Inject(testutil.ResultEvent(0.0)))
	waitForState(s.T(), worker, StateIdle)

	// Worker → Lead
	s.Require().NoError(worker.Query(context.Background(), "task 2", nil))

	input2, _ := json.Marshal(map[string]string{"to": "Lead", "message": "worker says hi"})
	s.Require().NoError(workerMock.Inject(testutil.ToolUseEvent("tu_w2l", ChannelSendMessageTool, json.RawMessage(input2))))
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

// TestChannel_LeaveChannel_StopsRouting verifies that after a session leaves a channel,
// SendMessage tool calls from that session are no longer routed to peers.
func (s *ChannelSuite) TestChannel_LeaveTeam_StopsRouting() {
	leadID, leadMock := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	channelID := s.createChannelWithMembers("leave-team", leadID, workerID)

	// Verify routing works before leaving.
	err := s.svc.RouteAgentMessage(context.Background(), AgentMessagePayload{
		SenderSessionID: leadID,
		TargetSessionID: workerID,
		Content:         "before leave",
	})
	s.Require().NoError(err)
	s.Require().Len(s.findAgentMessages(workerID), 1)

	// Leave the channel.
	s.Require().NoError(s.svc.LeaveChannel(context.Background(), leadID, channelID))

	// Inject a SendMessage ToolUseEvent — it should not route anywhere.
	lead := s.mgr.Get(leadID)
	s.Require().NoError(lead.Query(context.Background(), "more work", nil))

	input, _ := json.Marshal(map[string]string{"to": "Worker", "message": "after leave"})
	s.Require().NoError(leadMock.Inject(testutil.ToolUseEvent("tu_left", ChannelSendMessageTool, json.RawMessage(input))))
	time.Sleep(200 * time.Millisecond)
	s.Require().NoError(leadMock.Inject(testutil.ResultEvent(0.01)))
	waitForState(s.T(), lead, StateIdle)

	// Worker should still only have the 1 message from before leaving.
	workerMsgs := s.findAgentMessages(workerID)
	s.Len(workerMsgs, 1, "no new messages should arrive after leaving channel")
	s.Equal("before leave", workerMsgs[0].Content)
}

// TestChannel_Resume_RewireCallback verifies that when a session in a channel is
// stopped and resumed, the agent message callback is re-wired and messages
// route correctly again.
func (s *ChannelSuite) TestChannel_Resume_RewireCallback() {
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	s.createChannelWithMembers("resume-team", leadID, workerID)

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
	s.Require().NoError(resumedMock.Inject(testutil.ToolUseEvent("tu_resume", ChannelSendMessageTool, json.RawMessage(input))))
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

// TestChannel_Resume_ReplaysPendingDeliveries verifies that messages sent to an
// offline session are replayed when the session resumes.
func (s *ChannelSuite) TestChannel_Resume_ReplaysPendingDeliveries() {
	ctx := context.Background()
	leadID, _ := s.createNamedSession("Lead")
	workerID, _ := s.createNamedSession("Worker")
	channelID := s.createChannelWithMembers("replay-team", leadID, workerID)

	// Stop the worker so it's offline.
	s.Require().NoError(s.mgr.Stop(ctx, workerID))
	s.False(s.mgr.IsLive(workerID))

	// Send a message to the offline worker via SendChannelMessage.
	_, err := s.svc.SendChannelMessage(ctx, ChannelMessageParams{
		ChannelID:   channelID,
		SenderType:  "session",
		SenderID:    leadID,
		SenderName:  "Lead",
		Content:     "you missed this",
		MessageType: "message",
		Recipients:  []string{workerID},
	})
	s.Require().NoError(err)

	// Verify delivery is pending.
	pending, err := s.Queries.ListPendingDeliveriesForSession(ctx, workerID)
	s.Require().NoError(err)
	s.Require().Len(pending, 1)
	s.Equal("you missed this", pending[0].Content)

	// Give the worker a Claude session ID so resume works.
	s.Require().NoError(s.Queries.UpdateClaudeSessionID(ctx,
		store.UpdateClaudeSessionIDParams{
			ClaudeSessionID: sqlNullString("claude-replay-test"),
			ID:              workerID,
		}))

	// Resume the worker via QuerySession (triggers lazy resume).
	s.Require().NoError(s.svc.QuerySession(ctx, workerID, "I'm back", nil))
	s.True(s.mgr.IsLive(workerID))

	// Give the async replay goroutine time to deliver.
	time.Sleep(300 * time.Millisecond)

	// Verify: the pending message was delivered to the worker's CLI.
	workerMock := s.Connector.Last()
	var found bool
	for _, msg := range workerMock.SentMessages() {
		if contains(msg, "you missed this") {
			found = true
			break
		}
	}
	s.True(found, "worker CLI should have received the replayed message; got: %v", workerMock.SentMessages())

	// Verify: delivery status updated to "delivered".
	pending, err = s.Queries.ListPendingDeliveriesForSession(ctx, workerID)
	s.Require().NoError(err)
	s.Empty(pending, "no pending deliveries should remain after replay")
}

// --- M:N membership tests ---

func (s *ChannelSuite) TestChannel_JoinMultiple() {
	agentID, _ := s.createNamedSession("Agent")
	chA := s.createChannelWithMembers("alpha", agentID)
	chB := s.createChannelWithMembers("beta", agentID)

	// Session should be in both channels.
	memberships, err := s.Queries.ListSessionChannels(context.Background(), agentID)
	s.Require().NoError(err)
	s.Require().Len(memberships, 2)
	ids := map[string]bool{}
	for _, m := range memberships {
		ids[m.ChannelID] = true
	}
	s.True(ids[chA], "should be in channel alpha")
	s.True(ids[chB], "should be in channel beta")

	// Both channels should list the agent as a member.
	membersA, _ := s.Queries.ListChannelMemberSessions(context.Background(), chA)
	s.Require().Len(membersA, 1)
	s.Equal(agentID, membersA[0].ID)

	membersB, _ := s.Queries.ListChannelMemberSessions(context.Background(), chB)
	s.Require().Len(membersB, 1)
	s.Equal(agentID, membersB[0].ID)
}

func (s *ChannelSuite) TestChannel_LeaveOne_KeepsOther() {
	agentID, _ := s.createNamedSession("Agent")
	chA := s.createChannelWithMembers("alpha", agentID)
	chB := s.createChannelWithMembers("beta", agentID)

	// Leave channel A.
	s.Require().NoError(s.svc.LeaveChannel(context.Background(), agentID, chA))

	// Should still be in channel B.
	memberships, err := s.Queries.ListSessionChannels(context.Background(), agentID)
	s.Require().NoError(err)
	s.Require().Len(memberships, 1)
	s.Equal(chB, memberships[0].ChannelID)
}

func (s *ChannelSuite) TestChannel_DissolveOne_KeepsOther() {
	// Agent is in two channels. Dissolving one should not remove from the other.
	agentID, _ := s.createNamedSession("Agent")
	chA := s.createChannelWithMembers("alpha", agentID)
	chB := s.createChannelWithMembers("beta", agentID)

	// Dissolving alpha — since Agent is not a worker, it stays alive.
	s.Require().NoError(s.svc.DissolveChannel(context.Background(), chA))

	// Agent should still be in beta.
	memberships, err := s.Queries.ListSessionChannels(context.Background(), agentID)
	s.Require().NoError(err)
	s.Require().Len(memberships, 1)
	s.Equal(chB, memberships[0].ChannelID)
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
