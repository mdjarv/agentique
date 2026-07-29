package procctl

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstanceLockExcludesSecondHolder(t *testing.T) {
	dir := t.TempDir()

	first, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// The case the address probe missed: a second server against the same data
	// dir (any port, any DB) must be refused.
	_, err = AcquireInstanceLock(dir)
	if !errors.Is(err, ErrInstanceLocked) {
		t.Fatalf("second acquire: err = %v, want ErrInstanceLocked", err)
	}
	// The refusal names the holder so the operator knows what to stop.
	if pid := strconv.Itoa(os.Getpid()); !strings.Contains(err.Error(), pid) {
		t.Errorf("error %q does not name holding pid %s", err, pid)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// After release the dir is claimable again — no stale-lock wedge.
	second, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second: %v", err)
	}
}

func TestInstanceLockIsPerDataDir(t *testing.T) {
	a, err := AcquireInstanceLock(t.TempDir())
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	defer a.Release()

	// An isolated data dir (AGENTIQUE_HOME=<tmp> for a verify run) is a
	// different instance and must start freely alongside production.
	b, err := AcquireInstanceLock(t.TempDir())
	if err != nil {
		t.Fatalf("acquire b: %v", err)
	}
	if err := b.Release(); err != nil {
		t.Fatalf("release b: %v", err)
	}
}

func TestInstanceLockCreatesMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "agentique")
	l, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer l.Release()
	if _, err := os.Stat(filepath.Join(dir, instanceLockName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

func TestInstanceLockRejectsEmptyDir(t *testing.T) {
	if _, err := AcquireInstanceLock(""); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("err = %v, want ErrNoOwner", err)
	}
}

func TestReleaseNilLockIsSafe(t *testing.T) {
	var l *InstanceLock
	if err := l.Release(); err != nil {
		t.Fatalf("Release on nil lock: %v", err)
	}
}
