package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// Instance represents a running Chrome browser tied to a session.
type Instance struct {
	SessionID   string
	Port        int
	CDPEndpoint string // ws://localhost:PORT/devtools/browser/...
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	cdp         *CDPClient
}

// SetCDP sets the CDP client on the instance.
func (i *Instance) SetCDP(c *CDPClient) { i.cdp = c }

// CDP returns the CDP client for the instance, or nil.
func (i *Instance) CDP() *CDPClient { return i.cdp }

// Manager manages per-session Chrome browser instances.
type Manager struct {
	mu        sync.Mutex
	instances map[string]*Instance

	// chromePath is resolved once and cached.
	chromePath string

	// findChrome and execCommand are injectable for testing.
	findChrome  func() (string, error)
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewManager creates a new browser manager.
func NewManager() *Manager {
	return &Manager{
		instances:   make(map[string]*Instance),
		findChrome:  findChromeBinary,
		execCommand: exec.CommandContext,
	}
}

// chromeBinaries is the search order for Chrome/Chromium binaries.
var chromeBinaries = []string{
	"google-chrome-stable",
	"google-chrome",
	"chromium-browser",
	"chromium",
}

func findChromeBinary() (string, error) {
	for _, name := range chromeBinaries {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Chrome/Chromium binary found (tried: %v)", chromeBinaries)
}

// allocatePort finds a free TCP port by binding to :0 and releasing it.
func allocatePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// Launch starts a Chrome instance for the given session.
// Returns an error if one is already running for this session.
func (m *Manager) Launch(sessionID string) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.instances[sessionID]; ok && inst.cmd != nil {
		return inst, nil
	}

	if m.chromePath == "" {
		path, err := m.findChrome()
		if err != nil {
			return nil, err
		}
		m.chromePath = path
	}

	// Reuse pre-allocated port from placeholder if available.
	if inst, ok := m.instances[sessionID]; ok && inst.Port > 0 {
		return m.launchOnPort(sessionID, inst.Port)
	}

	port, err := allocatePort()
	if err != nil {
		return nil, err
	}

	return m.launchOnPort(sessionID, port)
}

// LaunchOnPort starts Chrome on a specific port (used for resume with a known port).
func (m *Manager) LaunchOnPort(sessionID string, port int) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.instances[sessionID]; ok && inst.cmd != nil {
		return inst, nil
	}

	if m.chromePath == "" {
		path, err := m.findChrome()
		if err != nil {
			return nil, err
		}
		m.chromePath = path
	}

	return m.launchOnPort(sessionID, port)
}

// launchOnPort requires m.mu to be held and m.chromePath to be set.
func (m *Manager) launchOnPort(sessionID string, port int) (*Instance, error) {
	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--headless=new",
		"--no-first-run",
		"--disable-default-apps",
		"--disable-gpu",
		"--disable-extensions",
		"--no-sandbox",
		fmt.Sprintf("--user-data-dir=/tmp/agentique-chrome-%s", sessionID),
	}

	cmd := m.execCommand(ctx, m.chromePath, args...)

	if err := cmd.Start(); err != nil {
		cancel()
		delete(m.instances, sessionID) // remove stale placeholder
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	inst := &Instance{
		SessionID: sessionID,
		Port:      port,
		cmd:       cmd,
		cancel:    cancel,
	}

	// Discover CDP endpoint with retries (Chrome needs time to open the debug port).
	cdpEndpoint, err := discoverCDPEndpoint(port, 30, 100*time.Millisecond)
	if err != nil {
		cancel()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		delete(m.instances, sessionID) // remove stale placeholder
		return nil, fmt.Errorf("discover CDP endpoint: %w", err)
	}
	inst.CDPEndpoint = cdpEndpoint

	m.instances[sessionID] = inst
	return inst, nil
}

// cdpTarget is a single entry from Chrome's /json endpoint.
type cdpTarget struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// discoverCDPEndpoint polls Chrome's /json endpoint until a page target appears.
// Returns the WebSocket URL for the first page target (not the browser target —
// page-level commands like Page.startScreencast require a page target).
func discoverCDPEndpoint(port int, maxRetries int, delay time.Duration) (string, error) {
	listURL := fmt.Sprintf("http://127.0.0.1:%d/json", port)
	client := &http.Client{Timeout: 2 * time.Second}

	var lastErr error
	for i := range maxRetries {
		resp, err := client.Get(listURL)
		if err != nil {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(delay)
			}
			continue
		}

		var targets []cdpTarget
		decErr := json.NewDecoder(resp.Body).Decode(&targets)
		resp.Body.Close()
		if decErr != nil {
			lastErr = decErr
			if i < maxRetries-1 {
				time.Sleep(delay)
			}
			continue
		}

		for _, t := range targets {
			if t.Type == "page" && t.WebSocketDebuggerURL != "" {
				return t.WebSocketDebuggerURL, nil
			}
		}

		lastErr = fmt.Errorf("no page target found (%d targets)", len(targets))
		if i < maxRetries-1 {
			time.Sleep(delay)
		}
	}
	return "", fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// Stop kills the Chrome process for the given session and removes it.
func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	inst, ok := m.instances[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.instances, sessionID)
	m.mu.Unlock()

	return stopInstance(inst)
}

func stopInstance(inst *Instance) error {
	if inst.cdp != nil {
		inst.cdp.Close()
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	if inst.cmd == nil {
		return nil
	}
	if inst.cmd.Process != nil {
		_ = inst.cmd.Process.Kill()
	}
	return inst.cmd.Wait()
}

// Get returns the instance for a session, or nil if not running.
func (m *Manager) Get(sessionID string) *Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.instances[sessionID]
}

// Port allocates a port for a session without launching Chrome.
// If already launched, returns the existing port.
func (m *Manager) Port(sessionID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.instances[sessionID]; ok {
		return inst.Port, nil
	}

	port, err := allocatePort()
	if err != nil {
		return 0, err
	}

	// Store a placeholder instance with just the port.
	m.instances[sessionID] = &Instance{
		SessionID: sessionID,
		Port:      port,
	}
	return port, nil
}

// StopAll kills all Chrome instances. Used on server shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		all = append(all, inst)
	}
	m.instances = make(map[string]*Instance)
	m.mu.Unlock()

	for _, inst := range all {
		_ = stopInstance(inst)
	}
}
