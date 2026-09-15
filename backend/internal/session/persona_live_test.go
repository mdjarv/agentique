package session

import (
	"context"
	"os"
	"testing"
	"time"

	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// LivePersonaSuite drives a real Claude CLI through the sessionless persona
// path, which is what the assistant's head and a web-only discussion persona
// run on.
//
// The hermetic suites cannot answer either question here, because both are
// properties of the CLI: which tools it actually offers a persona, and what it
// does when the model reaches for one that parks on a human.
//
// Gated: set AGENTIQUE_LIVE_PERSONA=1 and don't pass -short. Everything runs in
// a temp dir; no live agentique state is touched.
type LivePersonaSuite struct {
	testutil.DBSuite
	mgr *Manager
}

func TestLivePersonaSuite(t *testing.T) {
	t.Parallel()
	if testing.Short() || os.Getenv("AGENTIQUE_LIVE_PERSONA") == "" {
		t.Skip("live CLI test; set AGENTIQUE_LIVE_PERSONA=1 and drop -short")
	}
	suite.Run(t, new(LivePersonaSuite))
}

func (s *LivePersonaSuite) SetupTest() {
	s.DBSuite.SetupTest()
	s.mgr = NewManager(s.DB, s.Queries, s.Broadcaster,
		claudeadapter.NewConnector(ClaudeBaselineOptions()...))
	// Built the way serve builds them, for the same reason the ordinary one is.
	s.mgr.SetPersonaConnector(PersonaToolsNone, claudeadapter.NewConnector(
		append(ClaudeBaselineOptions(), ClaudePersonaOptions(PersonaToolsNone, 2*time.Minute)...)...))
	s.mgr.SetPersonaConnector(PersonaToolsWeb, claudeadapter.NewConnector(
		append(ClaudeBaselineOptions(), ClaudePersonaOptions(PersonaToolsWeb, 0)...)...))
}

func (s *LivePersonaSuite) TearDownTest() {
	if s.mgr != nil {
		s.mgr.CloseAll()
	}
}

// askPrompt makes the model reach for the question tool on its first move.
const askPrompt = "Before you write anything else, call the AskUserQuestion tool to ask me " +
	"whether I prefer tea or coffee. Whatever the tool answers, then reply with one short sentence."

// A persona has nobody to answer a question, so one must never park its turn:
// on 2026-09-15 the head called AskUserQuestion and waited out its whole
// ten-minute budget on an answer that could not come.
//
// No production route offers that tool any more, so this persona is spawned
// through a connector that deliberately leaves the CLI's own tool set in place
// — standing in for a CLI release that stops honouring --tools. The refusal is
// the layer that has to hold then.
func (s *LivePersonaSuite) TestAQuestionDoesNotParkTheTurn() {
	permissive := NewManager(s.DB, s.Queries, s.Broadcaster, &paramsConnector{})
	permissive.SetPersonaConnector(PersonaToolsWeb, claudeadapter.NewConnector(ClaudeBaselineOptions()...))
	defer permissive.CloseAll()

	rt, err := permissive.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		Preamble: "You are a test persona.",
		Model:    "haiku",
		WorkDir:  s.T().TempDir(),
		Tools:    PersonaToolsWeb,
	})
	s.Require().NoError(err)
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	started := time.Now()
	reply, err := rt.Query(ctx, askPrompt)
	s.T().Logf("reply after %s: %q (err %v)", time.Since(started).Round(time.Second), reply, err)
	s.Require().NoError(err, "the turn parked on a question nobody can answer")

	// The premise, checked rather than assumed: this persona is offered the
	// tool, so a pass above is the refusal working and not a CLI that simply
	// had no question to ask.
	tools, _ := rt.(*sessionlessPersona).offeredTools()
	s.Contains(tools, "AskUserQuestion")
}

// The head's set is offered nothing it was not handed. With no MCP config that
// is nothing at all — measured from the tool list the CLI itself reports at
// init, on the stream-json path the head runs on.
func (s *LivePersonaSuite) TestTheHeadsToolSetOffersNoToolOfItsOwn() {
	tools := s.offeredAfterATurn(PersonaToolsNone)
	s.Empty(tools, "a persona holding no native tool was offered tools of the CLI's own")
}

// A web-only persona is offered the two web tools and nothing else: no shell,
// no file tools, no question tool, and none of the operator's own connectors.
func (s *LivePersonaSuite) TestAWebPersonaIsOfferedOnlyTheWebTools() {
	tools := s.offeredAfterATurn(PersonaToolsWeb)
	s.ElementsMatch([]string{"WebSearch", "WebFetch"}, tools)
}

// offeredAfterATurn starts a persona with a set, runs the question prompt, and
// answers the tool list its CLI reported at init.
func (s *LivePersonaSuite) offeredAfterATurn(set PersonaTools) []string {
	rt, err := s.mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		Preamble: "You are a test persona.",
		Model:    "haiku",
		WorkDir:  s.T().TempDir(),
		Tools:    set,
	})
	s.Require().NoError(err)
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	reply, err := rt.Query(ctx, askPrompt)
	s.Require().NoError(err)
	s.T().Logf("%s reply: %q", set, reply)

	tools, inited := rt.(*sessionlessPersona).offeredTools()
	s.Require().True(inited, "the CLI never reported the tools it offers")
	return tools
}

// The web set still does the one thing it is for: a fetch goes through with
// nobody to approve it.
func (s *LivePersonaSuite) TestAWebPersonaCanStillReadTheWeb() {
	rt, err := s.mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		Preamble: "You are a test persona.",
		Model:    "haiku",
		WorkDir:  s.T().TempDir(),
		Tools:    PersonaToolsWeb,
	})
	s.Require().NoError(err)
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	reply, err := rt.Query(ctx, "Use WebFetch on https://example.com and tell me the page's main heading, "+
		"exactly as written.")
	s.Require().NoError(err)
	s.T().Logf("web fetch reply: %q", reply)
	s.Contains(reply, "Example Domain")
}
