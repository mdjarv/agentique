package machine

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// TailnetName returns this machine's MagicDNS name from `tailscale status
// --json` (Self.DNSName, trailing dot stripped), or "" when Tailscale is
// absent, logged out, or slow — callers treat that as a normal outcome, not
// an error. Deliberately CLI-only (no LocalAPI socket, no tsnet), and only
// reads Self: this is self-enumeration for display, not peer discovery.
func TailnetName() string {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}

	var status struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(status.Self.DNSName), ".")
}
