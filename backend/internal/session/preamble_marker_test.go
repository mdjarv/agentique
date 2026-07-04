package session

import (
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/procctl"
)

// TestPreambleCarriesReaperMarker locks the invariant that every session CLI's
// system prompt contains procctl.CLIProcessMarker. The orphan reaper
// (procctl.ReapOrphanedCLIProcesses / KillCLIChildrenOf) recognizes
// agentique-owned CLI processes solely by matching this substring in their
// command line (the preamble is passed via --append-system-prompt). If a
// preamble reword drops the marker, orphans would silently stop being reaped —
// so fail loudly here instead.
func TestPreambleCarriesReaperMarker(t *testing.T) {
	if !strings.Contains(preambleIdentity, procctl.CLIProcessMarker) {
		t.Fatalf("preambleIdentity must contain the reaper marker %q so orphaned CLI processes stay reap-able; preambleIdentity=%q",
			procctl.CLIProcessMarker, preambleIdentity)
	}

	// Both real and persona sessions derive from preambleIdentity — assert the
	// marker survives the full builders too.
	full := buildPreamble("sess", "branch", nil, BehaviorPresets{}, nil, nil, "", false, false, "")
	if !strings.Contains(full, procctl.CLIProcessMarker) {
		t.Errorf("buildPreamble output missing reaper marker %q", procctl.CLIProcessMarker)
	}
	persona := buildPersonaPreamble("", "")
	if !strings.Contains(persona, procctl.CLIProcessMarker) {
		t.Errorf("buildPersonaPreamble output missing reaper marker %q", procctl.CLIProcessMarker)
	}
}
