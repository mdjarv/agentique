package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// The agentique MCP config carries a per-session bearer token for /mcp, and
// claudecli passes each --mcp-config value straight into the CLI's argv. An
// argument is not a secret channel: /proc/<pid>/cmdline is world-readable, so
// every local user could read the token out of `ps` and then call the tools as
// that session — MemoryAdd in particular writes facts that later land in every
// agent's preamble.
//
// The CLI accepts "JSON files or strings" for --mcp-config, so hand it a path
// to an owner-only file instead. Only the path appears in argv.

// mcpConfigDir is where per-session MCP config files live. Inside the data
// directory, which is itself owner-only (paths.SecureDataDir).
func mcpConfigDir() string { return filepath.Join(paths.DataDir(), "mcp") }

func mcpConfigPath(sessionID string) string {
	return filepath.Join(mcpConfigDir(), sessionID+".json")
}

// writeSessionMCPConfig persists cfg for sessionID and returns the file path to
// pass to the CLI. Rewritten on every session start/resume, because the token
// is re-minted each time and the previous one is invalidated.
func writeSessionMCPConfig(sessionID, cfg string) (string, error) {
	// The id becomes a filename. Session ids are UUIDs; anything else is a bug
	// upstream, and this is not the place to find out by writing outside the dir.
	if uuid.Validate(sessionID) != nil {
		return "", fmt.Errorf("mcp config: session id %q is not a UUID", sessionID)
	}
	if err := os.MkdirAll(mcpConfigDir(), 0o700); err != nil {
		return "", fmt.Errorf("mcp config: create dir: %w", err)
	}
	path := mcpConfigPath(sessionID)
	// O_TRUNC over O_EXCL: a resume legitimately replaces the previous config.
	// The mode only applies on creation, so chmod covers a file left by an
	// earlier version.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("mcp config: open %s: %w", path, err)
	}
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("mcp config: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("mcp config: close %s: %w", path, err)
	}
	if err := paths.SecureFile(path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("mcp config: restrict %s: %w", path, err)
	}
	return path, nil
}

// removeSessionMCPConfig deletes a session's config file. Called alongside
// token revocation, so the file never outlives the credential it holds.
func removeSessionMCPConfig(sessionID string) {
	if uuid.Validate(sessionID) != nil {
		return
	}
	_ = os.Remove(mcpConfigPath(sessionID))
}
