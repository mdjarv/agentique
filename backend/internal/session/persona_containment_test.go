package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/testutil"
	"github.com/stretchr/testify/suite"
)

// A persona connects through the route for the tool set it names, and never
// through the ordinary connector, whose CLI carries every tool it has.
func TestAPersonaConnectsThroughItsToolSetsRoute(t *testing.T) {
	t.Parallel()
	ordinary, none, web := &paramsConnector{}, &paramsConnector{}, &paramsConnector{}
	mgr := NewManager(nil, nil, nil, ordinary)
	mgr.SetPersonaConnector(PersonaToolsNone, none)
	mgr.SetPersonaConnector(PersonaToolsWeb, web)

	for _, tools := range []PersonaTools{PersonaToolsNone, PersonaToolsWeb, PersonaToolsWeb} {
		rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
			WorkDir: t.TempDir(), Tools: tools,
		})
		if err != nil {
			t.Fatalf("start %s persona: %v", tools, err)
		}
		t.Cleanup(func() { _ = rt.Close() })
	}
	if len(none.params) != 1 || len(web.params) != 2 || len(ordinary.params) != 0 {
		t.Errorf("connected none=%d web=%d ordinary=%d; want 1, 2 and 0",
			len(none.params), len(web.params), len(ordinary.params))
	}
}

// A persona that names no set, an unknown set, or a set nobody wired a route
// for is refused before anything is spawned — never served by the ordinary
// connector.
func TestAPersonaWithoutARouteIsRefused(t *testing.T) {
	t.Parallel()
	ordinary, web := &paramsConnector{}, &paramsConnector{}
	mgr := NewManager(nil, nil, nil, ordinary)
	mgr.SetPersonaConnector(PersonaToolsWeb, web)

	for _, tools := range []PersonaTools{"", "everything", PersonaToolsNone} {
		rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
			WorkDir: t.TempDir(), Tools: tools,
		})
		if err == nil {
			_ = rt.Close()
			t.Errorf("a persona with tool set %q started", tools)
		}
	}
	if len(ordinary.params)+len(web.params) != 0 {
		t.Errorf("a refused start still connected: ordinary=%d web=%d", len(ordinary.params), len(web.params))
	}
}

// ContainedRouteSuite covers the one way a session could reach a persona
// route: by the provider name it carries.
type ContainedRouteSuite struct {
	testutil.DBSuite
}

func TestContainedRouteSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ContainedRouteSuite))
}

// The routes have no provider name, so no provider a session row carries can
// select one — including names that look as though they should.
func (s *ContainedRouteSuite) TestASessionCannotNameAPersonaRoute() {
	ordinary, none, web := &paramsConnector{}, &paramsConnector{}, &paramsConnector{}
	mgr := NewManager(s.DB, s.Queries, s.Broadcaster, ordinary)
	mgr.SetPersonaConnector(PersonaToolsNone, none)
	mgr.SetPersonaConnector(PersonaToolsWeb, web)
	s.T().Cleanup(mgr.CloseAll)

	for _, provider := range []string{"", "claude", "none", "web", "contained", "claude-contained", "persona"} {
		_, err := mgr.Create(context.Background(), CreateParams{
			ProjectID: s.Project.ID,
			Name:      "provider " + provider,
			WorkDir:   s.T().TempDir(),
			Model:     "opus",
			Provider:  provider,
		})
		s.Require().NoError(err, "provider %q", provider)
	}
	s.Empty(none.params, "a session reached the head's route by the provider it named")
	s.Empty(web.params, "a session reached the web persona's route by the provider it named")
}

// spawnedArgv starts a persona through a connector built from the given
// options around a stand-in CLI binary, and answers the argv it was spawned
// with and the MCP_TOOL_TIMEOUT it saw — read from the process rather than
// assumed from the option names.
func spawnedArgv(t *testing.T, tools PersonaTools, opts []claudecli.Option, mcpConfigs []string) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	argsFile, envFile := filepath.Join(dir, "args"), filepath.Join(dir, "env")
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + argsFile + "'\nprintf '%s' \"$MCP_TOOL_TIMEOUT\" > '" +
		envFile + "'\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	opts = append(opts, claudecli.WithBinaryPath(fake), claudecli.WithSkipVersionCheck(),
		claudecli.WithInitTimeout(2*time.Second))
	mgr := NewManager(nil, nil, nil, &paramsConnector{})
	mgr.SetPersonaConnector(tools, claudeadapter.NewConnector(opts...))
	rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		WorkDir: dir, Tools: tools, MCPConfigs: mcpConfigs,
	})
	if err == nil {
		_ = rt.Close()
	}

	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("the stand-in CLI was never spawned: %v (start error %v)", readErr, err)
	}
	env, _ := os.ReadFile(envFile)
	return strings.Split(string(bytes.TrimSuffix(raw, []byte{0})), "\x00"), string(env)
}

// What ClaudePersonaOptions puts on the head's command line: no native tool, no
// MCP server but its own (handed over as a path), and a tool timeout above the
// verbs' own deadline.
func TestTheHeadsCLIHoldsNoToolOfItsOwn(t *testing.T) {
	t.Parallel()
	mcpConfig := filepath.Join(t.TempDir(), "assistant-head-mcp.json")
	opts := append(ClaudeBaselineOptions(), ClaudePersonaOptions(PersonaToolsNone, 2*time.Minute)...)
	args, timeout := spawnedArgv(t, PersonaToolsNone, opts, []string{mcpConfig})

	if !hasFlagValue(args, "--tools", "") {
		t.Errorf(`argv carries no --tools "": the CLI keeps its own tool set. argv = %q`, args)
	}
	assertNoForeignTools(t, args)
	if !hasFlagValue(args, "--mcp-config", mcpConfig) {
		t.Errorf("argv does not hand the MCP config over as its path. argv = %q", args)
	}
	if timeout != "120000" {
		t.Errorf("MCP_TOOL_TIMEOUT = %q, want 120000: the CLI would give up on a tool call at its own 60s", timeout)
	}
}

// A web-only discussion persona holds the two web tools and nothing else.
func TestAWebPersonasCLIHoldsOnlyTheWebTools(t *testing.T) {
	t.Parallel()
	opts := append(ClaudeBaselineOptions(), ClaudePersonaOptions(PersonaToolsWeb, 0)...)
	args, timeout := spawnedArgv(t, PersonaToolsWeb, opts, nil)

	if !hasFlagValue(args, "--tools", "WebSearch,WebFetch") {
		t.Errorf(`argv carries no --tools "WebSearch,WebFetch". argv = %q`, args)
	}
	assertNoForeignTools(t, args)
	if timeout != "" {
		t.Errorf("MCP_TOOL_TIMEOUT = %q, want the CLI's own: a web persona is handed no MCP tools", timeout)
	}
}

// assertNoForeignTools checks the two options every persona set shares.
func assertNoForeignTools(t *testing.T, args []string) {
	t.Helper()
	if !slices.Contains(args, "--strict-mcp-config") {
		t.Errorf("argv carries no --strict-mcp-config: the user's MCP servers come along. argv = %q", args)
	}
	if !slices.Contains(args, "--disable-slash-commands") {
		t.Errorf("argv carries no --disable-slash-commands. argv = %q", args)
	}
}

// hasFlagValue reports whether args carries flag immediately followed by value.
func hasFlagValue(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
