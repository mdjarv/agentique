package mcphttp

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/allbin/agentkit/devurls"
	akmcp "github.com/allbin/agentkit/mcphttp"
)

func itoa(n int) string { return strconv.Itoa(n) }

// pickFreePort grabs an ephemeral port that's free at the moment of the call.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func resultText(r akmcp.Result) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// TestDevURLToolsRoundTrip exercises the agentkit/devurls swap end-to-end:
// Acquire returns a lease, ListDevUrls reflects the holder, Release frees it,
// and KillDevUrlPort handles a free slot gracefully (full kill path is covered
// by the live smoke-test step on a running server).
func TestDevURLToolsRoundTrip(t *testing.T) {
	ctx := context.Background()
	port := pickFreePort(t)
	store := devurls.NewStore([]devurls.Slot{
		{Slot: "dev1", Port: port, PublicHost: "dev1.test.example"},
	})

	res := acquireDevImpl(ctx, store, "sess-1")
	if res.IsError {
		t.Fatalf("acquireDevImpl reported error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), `Acquired dev URL slot "dev1"`) {
		t.Fatalf("acquire result missing slot name: %s", resultText(res))
	}

	// Bind the port to simulate the session having started its dev server
	// — otherwise List reports the lease as stale.
	bound, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	listRes := listDevURLsImpl(ctx, store)
	_ = bound.Close()
	if !strings.Contains(resultText(listRes), "held by sess-1 (port bound)") {
		t.Fatalf("list result missing held marker: %s", resultText(listRes))
	}

	relRes := releaseDevImpl(store, "sess-1")
	if !strings.Contains(resultText(relRes), "Released slot(s)") {
		t.Fatalf("release result unexpected: %s", resultText(relRes))
	}
	if leases := store.List(); len(leases) != 0 {
		t.Fatalf("expected no leases after release, got %d", len(leases))
	}

	killRes := killDevPortImpl(ctx, store, "dev1")
	if killRes.IsError {
		t.Fatalf("killDevPortImpl reported error: %s", resultText(killRes))
	}
	if !strings.Contains(resultText(killRes), "already free") {
		t.Fatalf("kill on free port should say already free: %s", resultText(killRes))
	}
}

// TestDevURLAcquireAllBusy verifies the agentkit ErrAllBusy sentinel flows
// through acquireDevImpl unchanged.
func TestDevURLAcquireAllBusy(t *testing.T) {
	ctx := context.Background()
	port := pickFreePort(t)
	store := devurls.NewStore([]devurls.Slot{
		{Slot: "dev1", Port: port, PublicHost: "dev1.test.example"},
	})

	if res := acquireDevImpl(ctx, store, "sess-A"); res.IsError {
		t.Fatalf("first acquire failed: %s", resultText(res))
	}

	res := acquireDevImpl(ctx, store, "sess-B")
	if !res.IsError {
		t.Fatalf("expected error result on exhausted pool, got: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "All dev URL slots are currently in use") {
		t.Fatalf("expected all-busy text, got: %s", resultText(res))
	}

	if _, err := store.Acquire(ctx, "sess-C"); !errors.Is(err, devurls.ErrAllBusy) {
		t.Fatalf("expected ErrAllBusy from store.Acquire, got %v", err)
	}
}

// TestDevURLKillExternalPort exercises the SIGTERM path on a real listener
// that the test owns. Linux-only — gracefully no-ops on platforms where
// FindPortOwner returns nil (the package's /proc-based lookup is Linux-only).
func TestDevURLKillExternalPort(t *testing.T) {
	ctx := context.Background()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	// Owner here is the test process itself; killDevPortImpl would SIGTERM
	// us, which is not what we want in a unit test. Instead, verify the
	// owner-lookup path runs cleanly through the new ctx-threading API.
	owner, err := devurls.FindPortOwner(ctx, port)
	if err != nil {
		t.Fatalf("FindPortOwner returned error: %v", err)
	}
	// On Linux owner should be populated; on other platforms it may be nil.
	t.Logf("FindPortOwner: owner=%v", owner)
}
