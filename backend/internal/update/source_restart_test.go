package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// "Restart to finish" needs no checkout and no source-apply. A machine that ran
// `just install` without restarting has a newer binary on disk whether or not it
// names a checkout, and restarting into it compiles nothing — so gating it on
// either left a paired machine silent in exactly that state.

func stagedBinary(t *testing.T, version string) string {
	t.Helper()
	fake := filepath.Join(t.TempDir(), "fake-agentique")
	script := "#!/bin/sh\necho 'agentique " + version + "'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestSourceStagedBinaryDetectedWithNoCheckoutConfigured(t *testing.T) {
	sc := NewSourceChecker(SourceOptions{
		BuiltFrom:   "abc1234",
		Version:     "v0.1.0-1-gabc1234",
		InstallPath: stagedBinary(t, "v0.1.0-5-gdef5678"),
		Origin:      OriginLocal,
	})
	st := sc.Refresh(context.Background())
	if !st.Staged || st.InstalledVersion != "v0.1.0-5-gdef5678" {
		t.Fatalf("staged = %v, installed = %q; want the install path's binary reported", st.Staged, st.InstalledVersion)
	}
	if st.Behind || st.Ahead != 0 {
		t.Fatalf("with no checkout there is no branch to be behind: %+v", st)
	}
	if st.Blocker != "no source checkout is configured" {
		t.Fatalf("blocker = %q", st.Blocker)
	}
}

func TestRestartIsAllowedWithoutACheckoutOrSourceApply(t *testing.T) {
	fr := newFakeRelease(t, "v0.2.0", []byte("binary"))
	h := newHarness(t, fr, "v0.1.0-1-gabc1234")
	sc := NewSourceChecker(SourceOptions{
		BuiltFrom:   "abc1234",
		Version:     "v0.1.0-1-gabc1234",
		InstallPath: stagedBinary(t, "v0.1.0-5-gdef5678"),
		Origin:      OriginLocal,
	})
	sc.Refresh(context.Background())
	h.applier.SetSource(sc, false)

	if _, err := h.applier.PreflightSource(); !errors.Is(err, ErrNoSource) {
		t.Fatalf("PreflightSource with no checkout = %v, want ErrNoSource", err)
	}
	if _, err := h.applier.PreflightRestart(); err != nil {
		t.Fatalf("PreflightRestart = %v, want a restart to be offered", err)
	}

	if err := h.applier.Start(KindRestart, "", false); err != nil {
		t.Fatalf("Start(restart) = %v", err)
	}
	select {
	case <-h.restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("restart never happened")
	}
	if got, _ := os.ReadFile(h.target); string(got) != "the old binary" {
		t.Fatalf("a restart must install nothing, target now %q", got)
	}
}

func TestBuildWaitsForSourceApply(t *testing.T) {
	fr := newFakeRelease(t, "v0.2.0", []byte("binary"))
	h := newHarness(t, fr, "v0.1.0-1-gabc1234")
	sc := NewSourceChecker(SourceOptions{
		Dir:       t.TempDir(),
		BuiltFrom: "abc1234",
		Origin:    OriginLocal,
	})
	h.applier.SetSource(sc, false)

	if _, err := h.applier.PreflightSource(); !errors.Is(err, ErrSourceBuildOff) {
		t.Fatalf("PreflightSource without source-apply = %v, want ErrSourceBuildOff", err)
	}
}
