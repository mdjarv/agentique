package session

import (
	"context"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type DiscussionSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestDiscussionSuite(t *testing.T) {
	suite.Run(t, new(DiscussionSuite))
}

func (s *DiscussionSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// seedGlobalProfile creates a project-less agent profile and returns its id.
func (s *DiscussionSuite) seedGlobalProfile(name, role, config string) string {
	ap, err := s.Queries.CreateAgentProfile(context.Background(), store.CreateAgentProfileParams{
		ID:     name + "-id",
		Name:   name,
		Role:   role,
		Config: config,
		// ProjectID left zero (NULL) → global persona, usable by web-only groups.
	})
	s.Require().NoError(err)
	return ap.ID
}

// driveTurn waits until the mock has received a query (so the per-turn capture
// channel is installed) then injects a turn-complete event with the given text.
func (s *DiscussionSuite) driveTurn(mock *testutil.MockCLISession, text string, priorQueries int) {
	s.Require().Eventually(func() bool {
		return len(mock.Queries()) > priorQueries
	}, 3*time.Second, 5*time.Millisecond, "persona was never queried")
	s.Require().NoError(mock.Inject(runtime.TurnCompletedEvent{
		Status:     runtime.TurnStatusCompleted,
		StopReason: "end_turn",
		Text:       text,
	}))
}

// TestStartPersonaRuntime drives the sessionless persona runtime directly: it
// must spawn no DB session row, run a turn, return the (trimmed) turn-complete
// text, and close cleanly.
func (s *DiscussionSuite) TestStartPersonaRuntime_QueryAndClose() {
	ctx := context.Background()
	rt, err := s.mgr.StartPersonaRuntime(ctx, PersonaRuntimeParams{
		Preamble: "you are a test persona",
		Model:    "opus",
		WorkDir:  s.T().TempDir(),
	})
	s.Require().NoError(err)

	// No agentique sessions row is created for a sessionless persona.
	allSessions, err := s.Queries.ListAllSessions(ctx)
	s.Require().NoError(err)
	s.Empty(allSessions)

	mock := s.Connector.Last()
	s.Require().NotNil(mock)

	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, qerr := rt.Query(ctx, "What is virtue?")
		done <- result{text, qerr}
	}()

	s.driveTurn(mock, "  Virtue is knowledge.  ", 0)

	select {
	case r := <-done:
		s.Require().NoError(r.err)
		s.Equal("Virtue is knowledge.", r.text) // trimmed
	case <-time.After(3 * time.Second):
		s.Fail("Query did not return after turn-complete event")
	}

	s.Require().NoError(rt.Close())
}

// TestSendChannelMessage_PersonaSkipsLegacyEvent verifies the writeLegacy skip:
// a persona message (sender_id is an agent_profile id, not a session) must not
// write a legacy agent_message session_event.
func (s *DiscussionSuite) TestSendChannelMessage_PersonaSkipsLegacyEvent() {
	ctx := context.Background()
	ch, err := s.svc.CreateChannel(ctx, "", "general") // web-only: NULL project
	s.Require().NoError(err)

	_, err = s.svc.SendChannelMessage(ctx, ChannelMessageParams{
		ChannelID:   ch.ID,
		SenderType:  "persona",
		SenderID:    "some-profile-id",
		SenderName:  "Socrates",
		Content:     "Virtue is knowledge.",
		MessageType: "message",
	})
	s.Require().NoError(err)

	// Persisted to the messages table...
	msgs, err := s.svc.GetChannelTimeline(ctx, ch.ID)
	s.Require().NoError(err)
	s.Require().Len(msgs, 1)
	s.Equal("persona", msgs[0].SenderType)

	// ...but no legacy session_event written against the (nonexistent) sender.
	events, err := s.Queries.ListEventsBySession(ctx, "some-profile-id")
	s.Require().NoError(err)
	s.Empty(events, "persona message must not write a legacy session_event")
}

// TestStartDiscussion_WebOnly is the end-to-end web-only path: a project-less
// channel, sessionless personas (no sessions rows, no channel_members), persona
// intros + contributions posted as sender_type "persona".
func (s *DiscussionSuite) TestStartDiscussion_WebOnly() {
	ctx := context.Background()
	p1 := s.seedGlobalProfile("Socrates", "philosopher", `{"capabilities":["ethics"]}`)
	p2 := s.seedGlobalProfile("Razor", "skeptic", `{}`)

	info, err := s.svc.StartDiscussion(ctx, StartDiscussionParams{
		Scope:     DiscussionScopeWebOnly,
		GroupName: "ethics",
		Mode:      DiscussionParallel, // both personas queried against one snapshot
		Personas: []DiscussionPersonaSpec{
			{AgentProfileID: p1, Name: "Socrates"},
			{AgentProfileID: p2, Name: "Razor"},
		},
		Prompt: "What is justice?",
	})
	s.Require().NoError(err)
	s.Len(info.Personas, 2)
	s.Equal("web-only", info.Scope)
	s.Empty(info.ProjectID)

	// Channel is project-less, with no members and no backing sessions.
	ch, err := s.Queries.GetChannel(ctx, info.ChannelID)
	s.Require().NoError(err)
	s.False(ch.ProjectID.Valid, "web-only channel project_id must be NULL")

	members, err := s.Queries.ListChannelMemberSessions(ctx, info.ChannelID)
	s.Require().NoError(err)
	s.Empty(members, "web-only personas are not channel_members")

	allSessions, err := s.Queries.ListAllSessions(ctx)
	s.Require().NoError(err)
	s.Empty(allSessions, "web-only personas must not create sessions rows")

	// Two persona introductions are posted up front (sender_type "persona").
	s.Require().Eventually(func() bool {
		return s.countPersonaMessages(info.ChannelID, "introduction") == 2
	}, 3*time.Second, 10*time.Millisecond, "expected 2 persona intros")

	// Drive both persona turns to completion (parallel mode runs them together).
	mocks := s.Connector.Sessions()
	s.Require().Len(mocks, 2, "two sessionless persona subprocesses")
	s.driveTurn(mocks[0], "Justice is harmony.", 0)
	s.driveTurn(mocks[1], "Justice is a construct.", 0)

	// Both contributions land as sender_type "persona" messages.
	s.Require().Eventually(func() bool {
		return s.countPersonaMessages(info.ChannelID, "message") == 2
	}, 5*time.Second, 20*time.Millisecond, "expected 2 persona contributions")

	// No legacy session_events were written for either persona's agent_profile id.
	for _, pid := range []string{p1, p2} {
		events, err := s.Queries.ListEventsBySession(ctx, pid)
		s.Require().NoError(err)
		s.Empty(events)
	}

	s.Require().NoError(s.svc.StopDiscussion(ctx, info.ChannelID))
}

func (s *DiscussionSuite) countPersonaMessages(channelID, messageType string) int {
	msgs, err := s.svc.GetChannelTimeline(context.Background(), channelID)
	s.Require().NoError(err)
	n := 0
	for _, m := range msgs {
		if m.SenderType == "persona" && m.MessageType == messageType {
			n++
		}
	}
	return n
}
