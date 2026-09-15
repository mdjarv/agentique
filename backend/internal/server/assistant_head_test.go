package server

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/allbin/agentkit/runtime"
	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// recordingCLIConnector hands out mock CLI sessions and keeps what each Connect
// was asked for.
type recordingCLIConnector struct {
	mu     sync.Mutex
	params []runtime.ConnectParams
}

func (c *recordingCLIConnector) Connect(_ context.Context, p runtime.ConnectParams) (runtime.CLISession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.params = append(c.params, p)
	return testutil.NewMockCLISession(), nil
}

func (c *recordingCLIConnector) connects() []runtime.ConnectParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]runtime.ConnectParams(nil), c.params...)
}

// The head's containment is asked for here, because this is where its
// subprocess is: it runs fullAuto with untrusted agent text in every turn, so
// every tool it holds is one it can use with nobody agreeing. docs/assistant.md's
// security section states it cannot reach a main worktree or a paired machine,
// which is untrue of a CLI carrying a shell, an Agent tool or the user's own MCP
// servers. So the head goes through the contained route, with its own MCP
// endpoint as the only tools it is handed — still as a 0600 file path.
func TestTheHeadStartsContained(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	ordinary, web, contained := &recordingCLIConnector{}, &recordingCLIConnector{}, &recordingCLIConnector{}
	mgr := session.NewManager(nil, nil, nil, ordinary)
	mgr.SetPersonaConnector(session.PersonaToolsWeb, web)
	mgr.SetPersonaConnector(session.PersonaToolsNone, contained)
	heads := &assistantHeads{mgr: mgr, tokens: mcphttp.NewTokenStore(), mcpURL: "http://127.0.0.1:1/mcp/assistant"}

	rt, err := heads.StartHead(context.Background(), assistant.HeadParams{Preamble: "you are the head"})
	if err != nil {
		t.Fatalf("StartHead() = %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if n := len(ordinary.connects()) + len(web.connects()); n != 0 {
		t.Errorf("the head connected through a route that carries tools %d times", n)
	}
	got := contained.connects()
	if len(got) != 1 {
		t.Fatalf("the head connected through the contained route %d times, want 1", len(got))
	}
	if len(got[0].MCPConfigs) != 1 {
		t.Fatalf("MCPConfigs = %q, want the one config file", got[0].MCPConfigs)
	}
	info, err := os.Stat(got[0].MCPConfigs[0])
	if err != nil {
		t.Fatalf("the MCP config is not a file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("MCP config mode = %o, want 0600: it holds the head's bearer", perm)
	}
}

// With no route for its tool set the head does not start — it is not started
// with the CLI's own tool set, or the web persona's, instead.
func TestTheHeadIsNeverStartedUncontained(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	ordinary, web := &recordingCLIConnector{}, &recordingCLIConnector{}
	mgr := session.NewManager(nil, nil, nil, ordinary)
	mgr.SetPersonaConnector(session.PersonaToolsWeb, web)
	heads := &assistantHeads{mgr: mgr}

	rt, err := heads.StartHead(context.Background(), assistant.HeadParams{Preamble: "you are the head"})
	if err == nil {
		_ = rt.Close()
		t.Fatal("the head started with no contained route")
	}
	if n := len(ordinary.connects()) + len(web.connects()); n != 0 {
		t.Errorf("a route that carries tools was used %d times", n)
	}
}

// Not the server's own working directory, which is wherever the operator ran
// the service from.
func TestTheHeadRunsInItsOwnDirectory(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())

	dir, err := headWorkDir()
	if err != nil {
		t.Fatalf("headWorkDir() = %v", err)
	}
	if dir == "" {
		t.Fatal("headWorkDir() answered nothing, which is the server's own cwd")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", dir)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %o, want 0700 — it sits in the data dir, which is owner-only", perm)
	}
}

// The head's endpoint URL is derived from the session endpoint's rather than
// configured: there is one listener, and a second setting for the same port is
// a second thing to get wrong.
func TestAssistantMCPURLIsDerived(t *testing.T) {
	if got := assistantMCPURL("http://127.0.0.1:19201/mcp"); got != "http://127.0.0.1:19201"+assistantMCPPath {
		t.Errorf("assistantMCPURL() = %q, want the head's own path", got)
	}
	if got := assistantMCPURL(""); got != "" {
		t.Errorf("assistantMCPURL(\"\") = %q, want empty — that is the endpoint not being wired", got)
	}
}

// A server killed mid-turn leaves a head's config file under a uuid nothing
// reuses. The token in it is already dead, so this is litter — swept at boot
// from the serve command, never from a constructor.
func TestSweepHeadCredentialsTakesOnlyTheHeadsOwn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTIQUE_HOME", home)

	dir := filepath.Join(paths.DataDir(), "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(dir, assistant.HeadIDPrefix+"11111111-2222-3333-4444-555555555555.json")
	session := filepath.Join(dir, "66666666-7777-8888-9999-000000000000.json")
	for _, path := range []string{stale, session} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	n, err := SweepHeadCredentials()
	if err != nil {
		t.Fatalf("SweepHeadCredentials() = %v", err)
	}
	if n != 1 {
		t.Errorf("removed %d files, want the one head's", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale head credential survived the sweep")
	}
	if _, err := os.Stat(session); err != nil {
		t.Error("the sweep took a session's config file, which the session manager owns")
	}
}
