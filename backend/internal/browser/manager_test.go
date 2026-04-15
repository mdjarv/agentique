package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocatePort(t *testing.T) {
	port, err := allocatePort()
	require.NoError(t, err)
	assert.Greater(t, port, 0)

	// Allocating a second port should give a different result (almost always).
	port2, err := allocatePort()
	require.NoError(t, err)
	assert.NotEqual(t, port, port2)
}

func TestFindChromeBinary_NotFound(t *testing.T) {
	// With a restricted PATH, nothing should be found.
	m := NewManager()
	m.findChrome = func() (string, error) {
		return "", fmt.Errorf("no Chrome/Chromium binary found")
	}

	_, err := m.Launch("test-session")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no Chrome/Chromium binary found")
}

// pageTargetResponse returns a JSON target list with a single page target.
func pageTargetResponse(wsURL string) []cdpTarget {
	return []cdpTarget{{ID: "abc123", Type: "page", WebSocketDebuggerURL: wsURL}}
}

func TestDiscoverCDPEndpoint(t *testing.T) {
	expectedURL := "ws://127.0.0.1:9222/devtools/page/abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json" {
			json.NewEncoder(w).Encode(pageTargetResponse(expectedURL))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	require.NoError(t, err)

	endpoint, err := discoverCDPEndpoint(port, 3, 0)
	require.NoError(t, err)
	assert.Equal(t, expectedURL, endpoint)
}

func TestDiscoverCDPEndpoint_Retries(t *testing.T) {
	calls := 0
	expectedURL := "ws://127.0.0.1:9222/devtools/page/abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(pageTargetResponse(expectedURL))
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	require.NoError(t, err)

	endpoint, err := discoverCDPEndpoint(port, 5, 0)
	require.NoError(t, err)
	assert.Equal(t, expectedURL, endpoint)
	assert.Equal(t, 3, calls)
}

func TestDiscoverCDPEndpoint_AllRetriesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	parts := strings.Split(srv.URL, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	require.NoError(t, err)

	_, err = discoverCDPEndpoint(port, 3, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after 3 retries")
}

func TestManager_GetNonExistent(t *testing.T) {
	m := NewManager()
	assert.Nil(t, m.Get("nonexistent"))
}

func TestManager_StopNonExistent(t *testing.T) {
	m := NewManager()
	err := m.Stop("nonexistent")
	assert.NoError(t, err)
}

func TestManager_StopAll_Empty(t *testing.T) {
	m := NewManager()
	m.StopAll() // Should not panic.
}

func TestManager_Port(t *testing.T) {
	m := NewManager()

	port, err := m.Port("session-1")
	require.NoError(t, err)
	assert.Greater(t, port, 0)

	// Same session should return the same port.
	port2, err := m.Port("session-1")
	require.NoError(t, err)
	assert.Equal(t, port, port2)

	// Different session should get a different port.
	port3, err := m.Port("session-2")
	require.NoError(t, err)
	assert.NotEqual(t, port, port3)
}

func TestManager_StopPlaceholder(t *testing.T) {
	m := NewManager()

	// Allocate a port (creates a placeholder with no cmd/cancel).
	_, err := m.Port("test")
	require.NoError(t, err)

	// Stopping a placeholder must not panic.
	err = m.Stop("test")
	assert.NoError(t, err)

	// Instance should be removed.
	assert.Nil(t, m.Get("test"))
}

func TestManager_LaunchIdempotent(t *testing.T) {
	expectedURL := "ws://127.0.0.1:9222/devtools/page/abc123"

	m := NewManager()
	m.findChrome = func() (string, error) { return "/bin/true", nil }
	m.execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "60")
	}

	// Pre-insert a fully-launched instance to test idempotency.
	// cmd must be non-nil to distinguish from a port-only placeholder.
	m.mu.Lock()
	m.instances["test"] = &Instance{
		SessionID:   "test",
		Port:        9222,
		CDPEndpoint: expectedURL,
		cmd:         exec.Command("true"),
		cancel:      func() {},
	}
	m.mu.Unlock()

	inst, err := m.Launch("test")
	require.NoError(t, err)
	assert.Equal(t, expectedURL, inst.CDPEndpoint)
}
