package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testSessionID = "11111111-2222-3333-4444-555555555555"

func TestWriteSessionMCPConfigIsOwnerOnly(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	cfg := AgentiqueMCPHTTPConfig("http://127.0.0.1:9201/mcp", "s3cret-token")

	path, err := writeSessionMCPConfig(testSessionID, cfg)
	if err != nil {
		t.Fatalf("writeSessionMCPConfig: %v", err)
	}
	if !strings.HasSuffix(path, testSessionID+".json") {
		t.Errorf("path = %q, want it named after the session", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The whole point: the token lives in the file, so a reader of argv sees
	// only this path.
	if !strings.Contains(string(data), "s3cret-token") {
		t.Error("config file does not contain the token")
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("config file is not valid JSON: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("file mode = %o, want 600", got)
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if got := dir.Mode().Perm(); got != 0o700 {
			t.Errorf("dir mode = %o, want 700", got)
		}
	}
}

func TestWriteSessionMCPConfigRewritesOnResume(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	first, err := writeSessionMCPConfig(testSessionID, `{"first":true}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeSessionMCPConfig(testSessionID, `{"second":true}`)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if first != second {
		t.Errorf("path changed across resume: %q then %q", first, second)
	}
	data, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	// A resume re-mints the token and invalidates the old one, so a stale
	// config left behind would just be a dead credential on disk.
	if got := string(data); got != `{"second":true}` {
		t.Errorf("content = %q, want the newest config", got)
	}
}

func TestWriteSessionMCPConfigRejectsNonUUIDSessionID(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	if _, err := writeSessionMCPConfig("../../escape", `{}`); err == nil {
		t.Error("a non-UUID session id must not become a filename")
	}
}

func TestRemoveSessionMCPConfig(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	path, err := writeSessionMCPConfig(testSessionID, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	removeSessionMCPConfig(testSessionID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("config file survived removal: %v", err)
	}
	// Idempotent — releaseSessionResources can run more than once.
	removeSessionMCPConfig(testSessionID)
}
