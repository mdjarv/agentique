package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// releaseServer serves one release payload, honouring If-None-Match so the
// ETag path is exercised for real rather than mocked.
type releaseServer struct {
	*httptest.Server
	calls    atomic.Int32
	notMod   atomic.Int32
	tag      atomic.Value // string
	assets   atomic.Value // []Asset
	failWith atomic.Int32 // non-zero: answer this status instead
}

const testETag = `W/"abc123"`

func newReleaseServer(t *testing.T, tag string, assets []Asset) *releaseServer {
	t.Helper()
	rs := &releaseServer{}
	rs.tag.Store(tag)
	rs.assets.Store(assets)
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.calls.Add(1)
		if code := rs.failWith.Load(); code != 0 {
			w.WriteHeader(int(code))
			fmt.Fprint(w, `{"message":"boom"}`)
			return
		}
		if r.Header.Get("If-None-Match") == testETag {
			rs.notMod.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", testETag)
		_ = json.NewEncoder(w).Encode(Release{
			TagName: rs.tag.Load().(string),
			Body:    "notes",
			HTMLURL: "https://example.test/release",
			Assets:  rs.assets.Load().([]Asset),
		})
	}))
	t.Cleanup(rs.Close)
	return rs
}

func linuxAssets() []Asset {
	return []Asset{
		{Name: "agentique-linux-amd64", URL: "https://example.test/agentique-linux-amd64", Size: 33 << 20},
		{Name: "checksums.txt", URL: "https://example.test/checksums.txt"},
	}
}

func newTestChecker(t *testing.T, srv *releaseServer, version, goos, goarch string) *Checker {
	t.Helper()
	return NewChecker(Options{
		Version:            version,
		APIURL:             srv.URL,
		GOOS:               goos,
		GOARCH:             goarch,
		MinRefreshInterval: time.Nanosecond,
	})
}

func TestStatusBeforeAnyCheck(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := newTestChecker(t, srv, "v0.4.1", "linux", "amd64")

	st := c.Status()
	if srv.calls.Load() != 0 {
		t.Fatal("Status must not touch the network")
	}
	if st.Latest != "" || st.Behind || st.CheckedAt != "" {
		t.Fatalf("unchecked status should be empty, got %+v", st)
	}
	if st.Current != "v0.4.1" || st.Channel != ChannelRelease {
		t.Fatalf("current/channel wrong: %+v", st)
	}
}

func TestRefreshReportsBehind(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := newTestChecker(t, srv, "v0.4.1", "linux", "amd64")

	st := c.Refresh(context.Background())
	if !st.Behind {
		t.Fatalf("v0.4.1 vs v0.5.0 should be behind: %+v", st)
	}
	if st.Latest != "v0.5.0" || st.Asset != "agentique-linux-amd64" || !st.Supported {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.CheckedAt == "" || st.CheckError != "" {
		t.Fatalf("checkedAt/checkError wrong: %+v", st)
	}
}

func TestDevBuildNeverNags(t *testing.T) {
	srv := newReleaseServer(t, "v9.9.9", linuxAssets())
	c := newTestChecker(t, srv, "v0.4.1-7-gab12cd3-dirty", "linux", "amd64")

	st := c.Refresh(context.Background())
	if st.Channel != ChannelDev {
		t.Fatalf("channel = %q, want dev", st.Channel)
	}
	if st.Behind {
		t.Fatal("a dev build must never be told it is behind")
	}
}

func TestUnverifiedPlatformIsUnsupported(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", []Asset{
		{Name: "agentique-windows-amd64.exe", URL: "https://example.test/x"},
	})
	c := newTestChecker(t, srv, "v0.4.1", "windows", "amd64")

	st := c.Refresh(context.Background())
	if st.Asset != "agentique-windows-amd64.exe" {
		t.Fatalf("asset = %q", st.Asset)
	}
	if st.Supported {
		t.Fatal("windows publishes an asset but is not verified — apply must stay off")
	}
	if !st.Behind {
		t.Fatal("unsupported still reports behind — the row says 'manual upgrade', not 'up to date'")
	}
}

func TestMissingAssetIsUnsupported(t *testing.T) {
	// Verified platform, but the release never published our asset.
	srv := newReleaseServer(t, "v0.5.0", []Asset{{Name: "checksums.txt"}})
	c := newTestChecker(t, srv, "v0.4.1", "linux", "amd64")

	if st := c.Refresh(context.Background()); st.Supported {
		t.Fatal("no published asset means no button")
	}
}

func TestFailedCheckKeepsLastAnswer(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := newTestChecker(t, srv, "v0.4.1", "linux", "amd64")

	first := c.Refresh(context.Background())
	if first.Latest != "v0.5.0" {
		t.Fatalf("setup failed: %+v", first)
	}

	srv.failWith.Store(http.StatusForbidden) // rate-limited
	second := c.Refresh(context.Background())
	if second.Latest != "v0.5.0" || !second.Behind {
		t.Fatalf("a failed check must keep the cached answer: %+v", second)
	}
	if second.CheckError == "" {
		t.Fatal("a failed check must say so")
	}
	if second.CheckedAt == first.CheckedAt && first.CheckedAt == "" {
		t.Fatal("checkedAt should be stamped on failure too")
	}
}

func TestETagAvoidsRefetch(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := newTestChecker(t, srv, "v0.4.1", "linux", "amd64")

	c.Refresh(context.Background())
	c.Refresh(context.Background())
	c.Refresh(context.Background())

	if srv.calls.Load() != 3 {
		t.Fatalf("expected 3 requests, got %d", srv.calls.Load())
	}
	if srv.notMod.Load() != 2 {
		t.Fatalf("expected 2 conditional hits, got %d", srv.notMod.Load())
	}
	if st := c.Status(); st.Latest != "v0.5.0" || st.CheckError != "" {
		t.Fatalf("304 must be a confirmation, not an error: %+v", st)
	}
}

func TestRefreshCoalesces(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := NewChecker(Options{
		Version: "v0.4.1", APIURL: srv.URL, GOOS: "linux", GOARCH: "amd64",
		MinRefreshInterval: time.Hour,
	})
	for range 5 {
		c.Refresh(context.Background())
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("refreshes inside the min interval should coalesce, got %d requests", srv.calls.Load())
	}
}

func TestStartPollsAndStops(t *testing.T) {
	srv := newReleaseServer(t, "v0.5.0", linuxAssets())
	c := NewChecker(Options{
		Version: "v0.4.1", APIURL: srv.URL, GOOS: "linux", GOARCH: "amd64",
		Interval: 10 * time.Millisecond,
	})
	c.Start(context.Background())
	deadline := time.After(2 * time.Second)
	for c.Status().Latest == "" {
		select {
		case <-deadline:
			t.Fatal("poll loop never produced an answer")
		case <-time.After(5 * time.Millisecond):
		}
	}
	c.Stop()
	c.Stop() // idempotent
}
