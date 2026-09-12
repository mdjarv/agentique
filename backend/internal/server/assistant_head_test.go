package server

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// The head's containment is built here, because this is where its subprocess
// is: it runs fullAuto with untrusted agent text in every turn, so the native
// tools it holds are tools it can use with nobody agreeing. docs/assistant.md's
// security section states it cannot reach a main worktree or a paired machine,
// and a CLI carrying a shell beside the data directory makes that untrue.
func TestTheHeadHasNoNativeToolsOfItsOwn(t *testing.T) {
	for _, tool := range []string{
		"Bash",      // a shell is every other tool at once
		"Read",      // the data dir holds every paired machine's outbound bearer
		"Write",     // nothing it could write is anything it owns
		"Edit",      //
		"WebFetch",  // a way out for anything it read
		"WebSearch", //
		"Task",      // a subagent is a second context with its own tool set
	} {
		if !slices.Contains(headDisallowedTools, tool) {
			t.Errorf("the head is allowed %s; it reaches the world through the verb table and nothing else", tool)
		}
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
