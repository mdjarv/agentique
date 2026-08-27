package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeRelease is a stand-in GitHub release plus its downloadable assets.
type fakeRelease struct {
	*httptest.Server
	mu       sync.Mutex
	binary   []byte
	checksum string // overrides the real digest when non-empty (to force a mismatch)
	stall    chan struct{}
	tag      string
}

func newFakeRelease(t *testing.T, tag string, binary []byte) *fakeRelease {
	t.Helper()
	fr := &fakeRelease{binary: binary, tag: tag}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		base := fr.URL
		_ = json.NewEncoder(w).Encode(Release{
			TagName: fr.tag,
			HTMLURL: "https://example.test/r",
			Assets: []Asset{
				{Name: "agentique-linux-amd64", URL: base + "/dl/binary", Size: int64(len(fr.binary))},
				{Name: ChecksumsAsset, URL: base + "/dl/checksums"},
			},
		})
	})
	mux.HandleFunc("/dl/binary", func(w http.ResponseWriter, _ *http.Request) {
		fr.mu.Lock()
		stall, body := fr.stall, fr.binary
		fr.mu.Unlock()
		if stall != nil {
			_, _ = w.Write(body[:len(body)/2])
			w.(http.Flusher).Flush()
			<-stall
			return
		}
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/dl/checksums", func(w http.ResponseWriter, _ *http.Request) {
		fr.mu.Lock()
		sum, body := fr.checksum, fr.binary
		fr.mu.Unlock()
		if sum == "" {
			d := sha256.Sum256(body)
			sum = hex.EncodeToString(d[:])
		}
		fmt.Fprintf(w, "%s  agentique-linux-amd64\n", sum)
	})
	fr.Server = httptest.NewServer(mux)
	t.Cleanup(fr.Close)
	return fr
}

// harness is an applier pointed at a throwaway install dir. Nothing here can
// reach a real service manager or a real binary.
type harness struct {
	applier  *Applier
	target   string
	restarts chan struct{}
	events   chan Progress
	busy     []string
}

func newHarness(t *testing.T, fr *fakeRelease, current string) *harness {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "agentique")
	write(t, target, "the old binary")

	h := &harness{
		target:   target,
		restarts: make(chan struct{}, 4),
		events:   make(chan Progress, 256),
	}
	checker := NewChecker(Options{
		Version: current, APIURL: fr.URL + "/releases/latest",
		GOOS: "linux", GOARCH: "amd64", MinRefreshInterval: time.Nanosecond,
	})
	checker.Refresh(t.Context())

	h.applier = NewApplier(checker, Deps{
		BinaryPath:       func() (string, error) { return target, nil },
		Restart:          func() error { h.restarts <- struct{}{}; return nil },
		ServiceInstalled: func() bool { return true },
		BusyTurns:        func() []string { return h.busy },
		Publish: func(topic string, payload any) {
			if p, ok := payload.(Progress); ok && topic == ProgressTopic {
				h.events <- p
			}
		},
		MachineID: "machine-1",
		Client:    fr.Client(),
	})
	return h
}

// waitPhase blocks until the applier reaches phase, or fails the test.
func (h *harness) waitPhase(t *testing.T, phase Phase) Progress {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case p := <-h.events:
			if p.Phase == phase {
				return p
			}
		case <-deadline:
			cur := h.applier.Progress()
			t.Fatalf("never reached %s (stuck at %+v)", phase, cur)
		}
	}
}

func TestApplyInstallsAndRestarts(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("the new binary"))
	h := newHarness(t, fr, "v0.4.1")

	if err := h.applier.Start(KindRelease, "v9.9.9", false); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseRestarting)

	select {
	case <-h.restarts:
	case <-time.After(5 * time.Second):
		t.Fatal("service was never restarted")
	}

	assertFile(t, h.target, "the new binary")
	assertFile(t, h.target+PrevSuffix, "the old binary")

	// No temp files left in the install dir.
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(h.target), ".agentique-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("install left temp files: %v", leftovers)
	}
}

func TestApplyNarratesEveryPhase(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("payload"))
	h := newHarness(t, fr, "v0.4.1")

	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}

	var seen []Phase
	deadline := time.After(10 * time.Second)
	for {
		var p Progress
		select {
		case p = <-h.events:
		case <-deadline:
			t.Fatalf("never reached restarting (saw %v)", seen)
		}
		if p.MachineID != "machine-1" {
			t.Fatalf("progress must say which machine it is about: %+v", p)
		}
		if len(seen) == 0 || seen[len(seen)-1] != p.Phase {
			seen = append(seen, p.Phase)
		}
		if p.Phase == PhaseRestarting {
			break
		}
	}

	want := []Phase{PhaseQueued, PhaseDownloading, PhaseVerifying, PhaseReplacing, PhaseRestarting}
	if len(seen) != len(want) {
		t.Fatalf("phases = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("phases = %v, want %v", seen, want)
		}
	}
}

func TestChecksumMismatchAbortsBeforeInstalling(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("the new binary"))
	fr.mu.Lock()
	fr.checksum = "deadbeef" // the published digest disagrees with the bytes
	fr.mu.Unlock()
	h := newHarness(t, fr, "v0.4.1")

	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}
	p := h.waitPhase(t, PhaseFailed)
	if p.Error == "" {
		t.Fatal("a failed upgrade must say why")
	}

	// The whole point: nothing was installed.
	assertFile(t, h.target, "the old binary")
	if _, err := os.Stat(h.target + PrevSuffix); !os.IsNotExist(err) {
		t.Fatal("a rejected binary must not have moved the current one aside")
	}
	select {
	case <-h.restarts:
		t.Fatal("a failed verification must never restart the service")
	default:
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(h.target), ".agentique-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("rejected download left temp files: %v", leftovers)
	}
}

func TestCancelDuringDownload(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("a somewhat longer new binary payload"))
	fr.mu.Lock()
	fr.stall = make(chan struct{})
	fr.mu.Unlock()
	defer func() {
		fr.mu.Lock()
		close(fr.stall)
		fr.mu.Unlock()
	}()

	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseDownloading)

	if err := h.applier.Cancel(); err != nil {
		t.Fatalf("download must be cancellable: %v", err)
	}
	p := h.waitPhase(t, PhaseCancelled)
	if p.Error != "" {
		t.Errorf("a cancel is not an error: %q", p.Error)
	}
	assertFile(t, h.target, "the old binary")
}

func TestCancelIsRefusedOnceInstalled(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseRestarting)

	// Past the line: cancel would mean rollback, which is its own command.
	if err := h.applier.Cancel(); !errors.Is(err, ErrNotRunning) && !errors.Is(err, ErrTooLate) {
		t.Fatalf("cancel after install must be refused, got %v", err)
	}
}

func TestCancelWithNothingRunning(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Cancel(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("got %v, want ErrNotRunning", err)
	}
}

func TestPhaseCancellability(t *testing.T) {
	cancellable := map[Phase]bool{
		PhaseQueued: true, PhaseDownloading: true, PhaseVerifying: true,
		PhaseReplacing: false, PhaseRestarting: false,
		PhaseFailed: false, PhaseCancelled: false,
	}
	for phase, want := range cancellable {
		if got := phase.Cancellable(); got != want {
			t.Errorf("%s.Cancellable() = %v, want %v", phase, got, want)
		}
	}
}

func TestBusyRefusesUnlessForced(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a", "session-b"}

	err := h.applier.Start(KindRelease, "", false)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a busy machine must refuse: %v", err)
	}
	assertFile(t, h.target, "the old binary")

	// Override is allowed — it just has to be asked for.
	if err := h.applier.Start(KindRelease, "", true); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseRestarting)
	assertFile(t, h.target, "new")
}

func TestStaleExpectationIsRefused(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")

	if err := h.applier.Start(KindRelease, "v8.0.0", false); !errors.Is(err, ErrStale) {
		t.Fatalf("installing something other than what was asked for must be refused: %v", err)
	}
	assertFile(t, h.target, "the old binary")
}

func TestSecondApplyIsRefused(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("a somewhat longer new binary payload"))
	fr.mu.Lock()
	fr.stall = make(chan struct{})
	fr.mu.Unlock()
	defer func() {
		fr.mu.Lock()
		close(fr.stall)
		fr.mu.Unlock()
	}()

	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseDownloading)
	if err := h.applier.Start(KindRelease, "", false); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("got %v, want ErrAlreadyRunning", err)
	}
}

func TestPreflightRefusesUnverifiedPlatform(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	checker := NewChecker(Options{
		Version: "v0.4.1", APIURL: fr.URL + "/releases/latest",
		GOOS: "darwin", GOARCH: "arm64", MinRefreshInterval: time.Nanosecond,
	})
	checker.Refresh(t.Context())
	a := NewApplier(checker, Deps{
		BinaryPath:       func() (string, error) { return filepath.Join(t.TempDir(), "agentique"), nil },
		ServiceInstalled: func() bool { return true },
	})
	if _, err := a.Preflight(); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("got %v, want ErrNotSupported", err)
	}
}

func TestPreflightRefusesWithoutAService(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.applier.deps.ServiceInstalled = func() bool { return false }

	if _, err := h.applier.Preflight(); err == nil {
		t.Fatal("with nothing to restart the new binary, there must be no button")
	}
}

func TestProgressSurvivesForALaterReader(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Start(KindRelease, "", false); err != nil {
		t.Fatal(err)
	}
	h.waitPhase(t, PhaseRestarting)

	// Progress is state, not just events: a client that connects after the
	// fact — a reload, a second tab — still sees where it got to.
	p := h.applier.Progress()
	if p == nil || p.Phase != PhaseRestarting || p.Target != "v9.9.9" {
		t.Fatalf("progress state = %+v", p)
	}
	if p.Cancellable {
		t.Error("a restarting upgrade must not advertise a cancel button")
	}
}
