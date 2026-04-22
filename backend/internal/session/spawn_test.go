package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

type SpawnSuite struct {
	testutil.DBSuite
	svc *Service
	mgr *Manager
}

func TestSpawnSuite(t *testing.T) {
	suite.Run(t, new(SpawnSuite))
}

func (s *SpawnSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster, connectorAdapter{s.Connector})
	s.svc = NewService(s.mgr, s.Queries, s.Broadcaster, testutil.NewMockBlockingRunner())
}

// initGitRepo turns the project's temp dir into a valid git repo with an
// initial commit so CreateSession(Worktree: true) can branch from it.
func (s *SpawnSuite) initGitRepo() {
	dir := s.Project.Path
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		s.Require().NoError(cmd.Run(), "git %v", args)
	}
	readme := filepath.Join(dir, "README.md")
	s.Require().NoError(os.WriteFile(readme, []byte("# test\n"), 0o644))
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		s.Require().NoError(cmd.Run(), "git %v", args)
	}
}

func (s *SpawnSuite) seedSession(name string) string {
	result, err := s.svc.CreateSession(context.Background(), CreateSessionParams{
		ProjectID: s.Project.ID,
		Name:      name,
		Model:     "opus",
	})
	s.Require().NoError(err)
	return result.SessionID
}

func (s *SpawnSuite) seedChannelWithRole(name, sessionID, role string) string {
	ctx := context.Background()
	ch, err := s.svc.CreateChannel(ctx, s.Project.ID, name)
	s.Require().NoError(err)
	_, err = s.svc.JoinChannel(ctx, sessionID, ch.ID, role)
	s.Require().NoError(err)
	return ch.ID
}

// --- authorizeSpawn unit tests ---

func (s *SpawnSuite) TestAuthSpawn_NoChannels_ReturnsPrompt() {
	id := s.seedSession("Solo")
	d, _ := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		Workers: []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionPrompt, d, "session in no channels falls back to UI approval")
}

func (s *SpawnSuite) TestAuthSpawn_LeadAnyChannel_AutoApproves() {
	id := s.seedSession("Lead")
	s.seedChannelWithRole("team-a", id, "lead")

	d, _ := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		Workers: []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionAuto, d)
}

func (s *SpawnSuite) TestAuthSpawn_WorkerRole_Rejects() {
	id := s.seedSession("Worker")
	s.seedChannelWithRole("team-a", id, "worker")

	d, reason := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		Workers: []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionReject, d)
	s.Contains(reason, "lead", "reject reason should explain the rule")
}

func (s *SpawnSuite) TestAuthSpawn_TargetChannelLead_AutoApproves() {
	id := s.seedSession("Lead")
	chID := s.seedChannelWithRole("team-a", id, "lead")

	d, _ := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		ChannelID: chID,
		Workers:   []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionAuto, d)
}

func (s *SpawnSuite) TestAuthSpawn_TargetChannelWorker_Rejects() {
	id := s.seedSession("Worker")
	chID := s.seedChannelWithRole("team-a", id, "worker")

	d, reason := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		ChannelID: chID,
		Workers:   []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionReject, d)
	s.Contains(reason, "not a lead")
}

func (s *SpawnSuite) TestAuthSpawn_TargetChannelNotMember_Rejects() {
	id := s.seedSession("Outsider")
	otherID := s.seedSession("Other")
	chID := s.seedChannelWithRole("team-a", otherID, "lead")

	d, reason := s.svc.authorizeSpawn(context.Background(), id, SpawnWorkersRequest{
		ChannelID: chID,
		Workers:   []SpawnWorkerEntry{{Name: "W1", Prompt: "x"}},
	})
	s.Equal(SpawnDecisionReject, d)
	s.Contains(reason, "not a member")
}

// --- extendSwarm / audit-message tests ---

func (s *SpawnSuite) TestExecuteSpawn_WithChannelID_ExtendsExisting() {
	s.initGitRepo()
	ctx := context.Background()
	leadID := s.seedSession("Lead")
	chID := s.seedChannelWithRole("team-extend", leadID, "lead")

	err := s.svc.executeSpawn(ctx, leadID, s.Project.ID, SpawnWorkersRequest{
		ChannelID: chID,
		Workers: []SpawnWorkerEntry{
			{Name: "Added", Prompt: "do stuff"},
		},
	})
	s.Require().NoError(err)

	// The channel should now have 2 members: lead + the new worker.
	members, err := s.Queries.ListChannelMemberSessions(ctx, chID)
	s.Require().NoError(err)
	s.Require().Len(members, 2)

	// No *new* channel should have been created.
	channels, err := s.Queries.ListChannelsByProject(ctx, s.Project.ID)
	s.Require().NoError(err)
	s.Require().Len(channels, 1, "spawn with ChannelID must not create a new channel")

	// Spawn audit message present in the existing channel.
	timeline, err := s.svc.GetChannelTimeline(ctx, chID)
	s.Require().NoError(err)
	var spawnMsgs []WireChannelMessage
	for _, m := range timeline {
		if m.MessageType == "spawn" {
			spawnMsgs = append(spawnMsgs, m)
		}
	}
	s.Require().Len(spawnMsgs, 1, "one spawn audit message per executeSpawn call")
	s.Equal(leadID, spawnMsgs[0].SenderID)
	s.Contains(spawnMsgs[0].Content, "Added")
}

func (s *SpawnSuite) TestExecuteSpawn_NewChannel_EmitsAudit() {
	s.initGitRepo()
	ctx := context.Background()
	leadID := s.seedSession("Lead")

	err := s.svc.executeSpawn(ctx, leadID, s.Project.ID, SpawnWorkersRequest{
		ChannelName: "fresh",
		Workers: []SpawnWorkerEntry{
			{Name: "W1", Prompt: "p1"},
			{Name: "W2", Prompt: "p2"},
		},
	})
	s.Require().NoError(err)

	channels, err := s.Queries.ListChannelsByProject(ctx, s.Project.ID)
	s.Require().NoError(err)
	s.Require().Len(channels, 1)
	chID := channels[0].ID

	timeline, err := s.svc.GetChannelTimeline(ctx, chID)
	s.Require().NoError(err)
	var spawnMsgs []WireChannelMessage
	for _, m := range timeline {
		if m.MessageType == "spawn" {
			spawnMsgs = append(spawnMsgs, m)
		}
	}
	s.Require().Len(spawnMsgs, 1)
	s.Contains(spawnMsgs[0].Content, "2 worker")
}

// --- legacy session_events filter for spawn messages ---

func (s *SpawnSuite) TestSendChannelMessage_SpawnType_SkipsLegacyEvents() {
	ctx := context.Background()
	leadID := s.seedSession("Lead")
	chID := s.seedChannelWithRole("team-filter", leadID, "lead")

	_, err := s.svc.SendChannelMessage(ctx, ChannelMessageParams{
		ChannelID:   chID,
		SenderType:  "session",
		SenderID:    leadID,
		SenderName:  "Lead",
		Content:     "spawned workers",
		MessageType: "spawn",
	})
	s.Require().NoError(err)

	events, err := s.Queries.ListEventsBySession(ctx, leadID)
	s.Require().NoError(err)
	for _, e := range events {
		if e.Type == "agent_message" {
			s.Failf("legacy agent_message event written for spawn messageType", "event data: %s", e.Data)
		}
	}
}

// --- Hierarchy tests ---

func (s *SpawnSuite) TestSpawn_SetsParentSessionID() {
	s.initGitRepo()
	ctx := context.Background()
	leadID := s.seedSession("Lead")

	err := s.svc.executeSpawn(ctx, leadID, s.Project.ID, SpawnWorkersRequest{
		ChannelName: "hierarchy",
		Workers:     []SpawnWorkerEntry{{Name: "W1", Prompt: "p"}},
	})
	s.Require().NoError(err)

	children, err := s.Queries.ListChildSessions(ctx, sqlNullString(leadID))
	s.Require().NoError(err)
	s.Require().Len(children, 1)
	s.Equal("W1", children[0].Name)
	s.Require().True(children[0].ParentSessionID.Valid)
	s.Equal(leadID, children[0].ParentSessionID.String)
}

func (s *SpawnSuite) TestDeleteSession_CascadesToChildren() {
	s.initGitRepo()
	ctx := context.Background()
	leadID := s.seedSession("Lead")

	err := s.svc.executeSpawn(ctx, leadID, s.Project.ID, SpawnWorkersRequest{
		ChannelName: "cascade",
		Workers: []SpawnWorkerEntry{
			{Name: "W1", Prompt: "p"},
			{Name: "W2", Prompt: "p"},
		},
	})
	s.Require().NoError(err)

	// Sanity: 3 sessions exist (lead + 2 workers) with correct parent links.
	children, err := s.Queries.ListChildSessions(ctx, sqlNullString(leadID))
	s.Require().NoError(err)
	s.Require().Len(children, 2, "lead should have 2 children before delete")

	// Delete the lead — descendants must go with it.
	s.Require().NoError(s.svc.DeleteSession(ctx, leadID))

	// Lead is gone.
	_, err = s.Queries.GetSession(ctx, leadID)
	s.Require().Error(err)

	// Workers gone too.
	for _, c := range children {
		_, err := s.Queries.GetSession(ctx, c.ID)
		s.Require().Error(err, "worker %q should be deleted with parent", c.Name)
	}
}

// --- Ensure ListChannelsByProject still works (used by tests above) ---

var _ = store.Channel{}
