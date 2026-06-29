package brain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// The scheduled-consolidation loop must run a pass shortly after Start, not a full
// interval later: a bare NewTicker would defer the first pass by the whole interval and
// reset that clock on every restart, so a frequently-restarted server could postpone the
// semantic refresh indefinitely. We prove the near-boot pass with a huge interval (so the
// only way a pass can run inside the test window is the initial timer) and a tiny initial
// delay. The persisted fingerprint file is the observable side effect of a real pass.
func TestAutomationRunsOnceShortlyAfterStart(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// A scope must exist or the pass has nothing to consolidate (and writes no fingerprint).
	if _, err := svc.Add(context.Background(), ScopeForProject("p1"), "Project builds with just.", memory.CategoryProject, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	a := NewAutomation(svc, nil, eventbus.NopBroadcaster{}, time.Hour, "", 0, 0)
	a.initialDelay = 5 * time.Millisecond
	a.Start()
	defer a.Stop()

	fpPath := filepath.Join(dir, ".fingerprints.json")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(fpPath); err == nil {
			return // a consolidation pass ran before the (1h) interval — success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected a consolidation pass (fingerprint file %s) within the initial delay, none ran", fpPath)
}

// Stop before the initial delay fires must cancel the pending first pass cleanly and not
// leave the goroutine blocked or run a pass after teardown.
func TestAutomationStopBeforeInitialPass(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(context.Background(), ScopeForProject("p1"), "Project builds with just.", memory.CategoryProject, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	a := NewAutomation(svc, nil, eventbus.NopBroadcaster{}, time.Hour, "", 0, 0)
	a.initialDelay = time.Hour // long enough that Stop wins the race
	a.Start()
	a.Stop()

	// Give a cancelled loop a moment; no pass should run, so no fingerprint file appears.
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, ".fingerprints.json")); err == nil {
		t.Fatal("consolidation pass ran after Stop; the initial timer was not cancelled")
	}
}
