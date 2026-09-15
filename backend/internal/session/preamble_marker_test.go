package session

import (
	"context"
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
	t.Parallel()
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

// A persona's preamble is written by its caller, and the assistant's head wrote
// one without the marker, so its CLI was invisible to the reaper. The manager
// adds the marker to whatever it is given — once.
func TestPersonaRuntimeCarriesReaperMarker(t *testing.T) {
	t.Parallel()
	for _, preamble := range []string{"", "You are the assistant to a developer.", buildPersonaPreamble("", "")} {
		conn := &paramsConnector{}
		mgr := NewManager(nil, nil, nil, &paramsConnector{})
		mgr.SetPersonaConnector(PersonaToolsNone, conn)
		rt, err := mgr.StartPersonaRuntime(context.Background(),
			PersonaRuntimeParams{Preamble: preamble, WorkDir: t.TempDir(), Tools: PersonaToolsNone})
		if err != nil {
			t.Fatalf("start persona: %v", err)
		}
		_ = rt.Close()

		params, _ := conn.last()
		if n := strings.Count(params.Preamble, procctl.CLIProcessMarker); n != 1 {
			t.Errorf("preamble %q reached the CLI with the reaper marker %d times, want once: %q",
				preamble, n, params.Preamble)
		}
		if !strings.Contains(params.Preamble, preamble) {
			t.Errorf("the caller's preamble %q did not survive: %q", preamble, params.Preamble)
		}
	}
}
