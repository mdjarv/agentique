// Package machine provides the server's stable machine identity for the
// multi-machine feature (docs/multi-machine.md). The identity is a
// UUID persisted in the data directory — stable across restarts, ports, IPs,
// and access methods, so the same machine reached via LAN or tailnet is one
// machine, not several. Clients verify it together with the signing identity
// on every connect.
package machine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const idFileName = "machine-id"

// LoadOrCreateID returns the machine id persisted in dataDir, generating and
// persisting a new UUID on first run.
func LoadOrCreateID(dataDir string) (string, error) {
	path := filepath.Join(dataDir, idFileName)

	raw, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(raw))
		if _, parseErr := uuid.Parse(id); parseErr == nil {
			return id, nil
		}
		// Corrupt content falls through to regeneration rather than shipping a
		// broken identity that every client would then pin against.
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read machine id: %w", err)
	}

	id := uuid.New().String()
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist machine id: %w", err)
	}
	return id, nil
}

// Label returns the human-friendly machine name: the config override when set,
// else PRETTY_HOSTNAME from /etc/machine-info, else the OS hostname, else a
// fixed fallback. Never empty.
func Label(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	if v := prettyHostname(); v != "" {
		return v
	}
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return "agentique"
}

// prettyHostname reads PRETTY_HOSTNAME from /etc/machine-info (systemd
// convention). Returns "" when absent or unreadable.
func prettyHostname() string {
	raw, err := os.ReadFile("/etc/machine-info")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key != "PRETTY_HOSTNAME" {
			continue
		}
		return strings.Trim(strings.TrimSpace(val), `"`)
	}
	return ""
}
