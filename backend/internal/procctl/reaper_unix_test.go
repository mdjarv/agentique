//go:build !windows

package procctl

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadParentAndGroup(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		stat     string
		wantPPID int
		wantPGID int
		ok       bool
	}{
		{
			name:     "simple comm",
			stat:     "1234 (claude) S 4402 1234 1234 0 -1 4194304 ...",
			wantPPID: 4402,
			wantPGID: 1234,
			ok:       true,
		},
		{
			// comm containing spaces and parentheses — the reason we split on
			// the final ')' rather than field-splitting the whole line.
			name:     "comm with spaces and parens",
			stat:     "77 ((odd )comm)) S 1 55 55 0 -1 0 rest of fields",
			wantPPID: 1,
			wantPGID: 55,
			ok:       true,
		},
		{
			name: "truncated",
			stat: "77 (comm) S",
			ok:   false,
		},
		{
			name: "garbage",
			stat: "not a stat line",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name)
			if err := os.WriteFile(p, []byte(tc.stat), 0o644); err != nil {
				t.Fatal(err)
			}
			ppid, pgid, ok := readParentAndGroup(p)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if ppid != tc.wantPPID || pgid != tc.wantPGID {
				t.Fatalf("ppid=%d pgid=%d, want ppid=%d pgid=%d", ppid, pgid, tc.wantPPID, tc.wantPGID)
			}
		})
	}
}

func TestSignalGroupRefusesSelfAndInvalid(t *testing.T) {
	for _, pgid := range []int{0, 1, syscall.Getpgrp()} {
		if err := signalGroup(pgid, syscall.SIGTERM); err != errRefuseSelfGroup {
			t.Errorf("signalGroup(%d) = %v, want errRefuseSelfGroup", pgid, err)
		}
	}
}

// TestFindAndKillCLIProcess spawns a child whose command line carries the reaper
// marker in its own process group, then exercises the real /proc scan and the
// group-kill path end to end.
func TestFindAndKillCLIProcess(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc; reaper is Linux-specific")
	}
	// The marker lands in argv, so FindCLIProcesses matches it via /proc/<pid>/cmdline.
	script := "sleep 60 # " + CLIProcessMarker
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own group, like the CLI adapter
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})

	var found *CLIProcess
	for _, p := range FindCLIProcesses() {
		if p.PID == pid {
			p := p
			found = &p
			break
		}
	}
	if found == nil {
		t.Fatalf("FindCLIProcesses did not return spawned pid %d", pid)
	}
	if found.PPID != os.Getpid() {
		t.Errorf("PPID = %d, want %d (this test process)", found.PPID, os.Getpid())
	}
	if found.PGID != pid {
		t.Errorf("PGID = %d, want %d (own group leader)", found.PGID, pid)
	}

	// KillCLIChildrenOf(self) should SIGKILL the whole group.
	if n := KillCLIChildrenOf(os.Getpid()); n < 1 {
		t.Fatalf("KillCLIChildrenOf killed %d groups, want >=1", n)
	}
	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process still alive after group kill")
	}
}
