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

// A contained persona connects through the contained route, and an ordinary
// one does not.
func TestContainedPersonaConnectsThroughTheContainedRoute(t *testing.T) {
	t.Parallel()
	ordinary, contained := &paramsConnector{}, &paramsConnector{}
	mgr := NewManager(nil, nil, nil, ordinary)
	mgr.SetContainedConnector(contained)

	rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		WorkDir: t.TempDir(), Contained: true,
	})
	if err != nil {
		t.Fatalf("start contained persona: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	if len(contained.params) != 1 || len(ordinary.params) != 0 {
		t.Fatalf("contained start connected %d times contained, %d ordinary; want 1 and 0",
			len(contained.params), len(ordinary.params))
	}

	plain, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("start ordinary persona: %v", err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	if len(contained.params) != 1 || len(ordinary.params) != 1 {
		t.Errorf("ordinary start connected %d times contained, %d ordinary; want 1 and 1",
			len(contained.params), len(ordinary.params))
	}
}

// Asking for containment where there is none is refused before anything is
// spawned, never quietly served by the ordinary connector.
func TestContainedPersonaIsRefusedWithoutAContainedRoute(t *testing.T) {
	t.Parallel()
	ordinary := &paramsConnector{}
	mgr := NewManager(nil, nil, nil, ordinary)

	rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		WorkDir: t.TempDir(), Contained: true,
	})
	if err == nil {
		_ = rt.Close()
		t.Fatal("a contained persona started with no contained route")
	}
	if len(ordinary.params) != 0 {
		t.Errorf("the ordinary connector was used %d times for a contained start", len(ordinary.params))
	}
}

// ContainedRouteSuite covers the one way a session could reach the contained
// route: by the provider name it carries.
type ContainedRouteSuite struct {
	testutil.DBSuite
}

func TestContainedRouteSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ContainedRouteSuite))
}

// The route has no name, so no provider a session row carries can select it —
// including names that look as though they should.
func (s *ContainedRouteSuite) TestASessionCannotNameTheContainedRoute() {
	ordinary, contained := &paramsConnector{}, &paramsConnector{}
	mgr := NewManager(s.DB, s.Queries, s.Broadcaster, ordinary)
	mgr.SetContainedConnector(contained)
	s.T().Cleanup(mgr.CloseAll)

	for _, provider := range []string{"", "claude", "contained", "claude-contained", "persona"} {
		_, err := mgr.Create(context.Background(), CreateParams{
			ProjectID: s.Project.ID,
			Name:      "provider " + provider,
			WorkDir:   s.T().TempDir(),
			Model:     "opus",
			Provider:  provider,
		})
		s.Require().NoError(err, "provider %q", provider)
	}
	s.Empty(contained.params, "a session reached the contained connector by the provider it named")
}

// What ClaudeContainedOptions actually puts on the CLI's command line, read from
// a stand-in binary rather than assumed from the option names — and that the
// head's MCP config still reaches it as a path.
func TestContainedConnectorSpawnsTheCLIWithNoToolsOfItsOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "claude")
	envFile := filepath.Join(dir, "env")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > '" + argsFile + "'\nprintf '%s' \"$MCP_TOOL_TIMEOUT\" > '" +
		envFile + "'\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	opts := append(ClaudeBaselineOptions(), ClaudeContainedOptions(2*time.Minute)...)
	opts = append(opts, claudecli.WithBinaryPath(fake), claudecli.WithSkipVersionCheck(),
		claudecli.WithInitTimeout(2*time.Second))
	mgr := NewManager(nil, nil, nil, &paramsConnector{})
	mgr.SetContainedConnector(claudeadapter.NewConnector(opts...))

	mcpConfig := filepath.Join(dir, "assistant-head-mcp.json")
	rt, err := mgr.StartPersonaRuntime(context.Background(), PersonaRuntimeParams{
		WorkDir: dir, Contained: true, MCPConfigs: []string{mcpConfig},
	})
	if err == nil {
		_ = rt.Close()
	}

	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("the stand-in CLI was never spawned: %v (start error %v)", readErr, err)
	}
	args := strings.Split(string(bytes.TrimSuffix(raw, []byte{0})), "\x00")

	if !hasFlagValue(args, "--tools", "") {
		t.Errorf(`argv carries no --tools "": the CLI keeps its own tool set. argv = %q`, args)
	}
	if !slices.Contains(args, "--strict-mcp-config") {
		t.Errorf("argv carries no --strict-mcp-config: the user's MCP servers come along. argv = %q", args)
	}
	if !slices.Contains(args, "--disable-slash-commands") {
		t.Errorf("argv carries no --disable-slash-commands. argv = %q", args)
	}
	if !hasFlagValue(args, "--mcp-config", mcpConfig) {
		t.Errorf("argv does not hand the MCP config over as its path. argv = %q", args)
	}
	if env, _ := os.ReadFile(envFile); string(env) != "120000" {
		t.Errorf("MCP_TOOL_TIMEOUT = %q, want 120000: the CLI would give up on a tool call at its own 60s", env)
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
