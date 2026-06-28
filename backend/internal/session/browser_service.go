package session

import (
	"fmt"
	"log/slog"

	"github.com/allbin/agentkit/eventbus"
	"github.com/mdjarv/agentique/backend/internal/browser"
)

// BrowserService handles browser lifecycle operations for sessions.
type BrowserService struct {
	mgr       *Manager
	browserMgr *browser.Manager
	hub eventbus.Broadcaster
}

// NewBrowserService creates a BrowserService.
func NewBrowserService(mgr *Manager, browserMgr *browser.Manager, hub eventbus.Broadcaster) *BrowserService {
	return &BrowserService{mgr: mgr, browserMgr: browserMgr, hub: hub}
}

// MCPServerName is the MCP server name used for Agentique's managed browser.
// Distinct from "playwright" to avoid collisions with globally-installed plugins.
const MCPServerName = "agentique-playwright"

// StandalonePlaywrightMCPConfig returns the MCP config JSON for the always-on
// agent browser. It points @playwright/mcp at the session's pre-allocated CDP
// port — Chrome is launched lazily on first tool use, at which point the MCP
// connects over CDP — and at outputDir so screenshots taken without an explicit
// filename land where the API can serve them. No launch/profile flags: in
// --cdp-endpoint mode the MCP attaches to the agentique-managed Chrome rather
// than launching its own, so headless-ness and profile come from that launch.
func StandalonePlaywrightMCPConfig(port int, outputDir string) string {
	return fmt.Sprintf(`{"mcpServers":{%q:{"command":"npx","args":["@playwright/mcp","--cdp-endpoint","http://127.0.0.1:%d","--output-dir",%q]}}}`, MCPServerName, port, outputDir)
}

// AllocatePort pre-allocates a browser port for a session. Called during
// session creation so the MCP config can reference the port before Chrome starts.
func (bs *BrowserService) AllocatePort(sessionID string) (int, error) {
	return bs.browserMgr.Port(sessionID)
}

// EnsureBrowser makes sure a headless Chrome is running for the session so the
// agent's Playwright MCP — which connects lazily over CDP at tool-execution
// time — can attach. Idempotent and safe under concurrency (the underlying
// launch is). Auto-provisions a Chromium if the host has none. Does NOT start
// the screencast: that is the panel's job (LaunchBrowser). Never touches the CLI
// control channel — having Chrome up before the tool call is approved is
// sufficient; no MCP reconnect is needed.
func (bs *BrowserService) EnsureBrowser(sessionID string) error {
	sess := bs.mgr.Get(sessionID)
	if sess == nil {
		return ErrNotLive
	}

	// Auto-provision a browser binary if none is present (userspace, no root).
	if !bs.browserMgr.ChromeAvailable() {
		bs.hub.Publish(sess.ProjectID, "browser.provisioning", PushBrowserProvisioning{SessionID: sessionID, State: "installing"})
		if err := bs.browserMgr.ProvisionChrome(); err != nil {
			bs.hub.Publish(sess.ProjectID, "browser.provisioning", PushBrowserProvisioning{SessionID: sessionID, State: "failed"})
			return fmt.Errorf("no browser available and auto-install failed: %w", err)
		}
		bs.hub.Publish(sess.ProjectID, "browser.provisioning", PushBrowserProvisioning{SessionID: sessionID, State: "ready"})
	}

	port := sess.BrowserPort()
	var err error
	if port > 0 {
		_, err = bs.browserMgr.LaunchOnPort(sessionID, port)
	} else {
		_, err = bs.browserMgr.Launch(sessionID)
	}
	if err != nil {
		return fmt.Errorf("launch browser: %w (if Chrome is installed but won't start, install its system libraries with: npx playwright install-deps chromium)", err)
	}
	return nil
}

// LaunchBrowser opens the human-facing panel: it ensures Chrome is up (reusing
// the agent's instance if already running), connects agentique's own CDP client,
// and starts the screencast. The panel is a live view of the *same* browser the
// agent drives. Idempotent — a second call while screencasting is a no-op.
func (bs *BrowserService) LaunchBrowser(sessionID string) error {
	if err := bs.EnsureBrowser(sessionID); err != nil {
		return err
	}
	sess := bs.mgr.Get(sessionID)
	if sess == nil {
		return ErrNotLive
	}
	inst := bs.browserMgr.Get(sessionID)
	if inst == nil {
		return errNoBrowser
	}
	// Already attached + screencasting.
	if inst.CDP() != nil {
		return nil
	}

	// Connect agentique's CDP client for screencast + input. On failure leave
	// Chrome running — the agent may be using it; only the panel view is affected.
	cdpClient, err := browser.NewCDPClient(inst.CDPEndpoint)
	if err != nil {
		return fmt.Errorf("cdp connect: %w", err)
	}
	inst.SetCDP(cdpClient)

	projectID := sess.ProjectID
	cdpClient.SetOnFrame(func(frame browser.FrameEvent) {
		bs.hub.Publish(projectID, "browser.frame", PushBrowserFrame{
			SessionID: sessionID, Data: frame.Data, Metadata: frame.Metadata,
		})
	})

	if err := cdpClient.SetViewport(1280, 720); err != nil {
		slog.Warn("viewport override failed", "session_id", sessionID, "error", err)
	}
	if err := cdpClient.StartScreencast(80, 1280, 720); err != nil {
		return fmt.Errorf("screencast: %w", err)
	}

	slog.Info("browser panel attached", "session_id", sessionID, "port", inst.Port)
	return nil
}

// StopBrowser kills the Chrome process for the given session.
func (bs *BrowserService) StopBrowser(sessionID string) error {
	sess := bs.mgr.Get(sessionID)
	projectID := ""
	if sess != nil {
		projectID = sess.ProjectID
	}

	if err := bs.browserMgr.Stop(sessionID); err != nil {
		return err
	}

	if projectID != "" {
		bs.hub.Publish(projectID, "browser.stopped", PushBrowserStopped{SessionID: sessionID, Reason: "user-initiated"})
	}
	return nil
}

// BrowserInput dispatches a mouse or keyboard event to the session's browser.
func (bs *BrowserService) BrowserInput(sessionID string, input BrowserInputParams) error {
	cdp, err := bs.requireCDP(sessionID)
	if err != nil {
		return err
	}

	switch input.InputType {
	case "mouse":
		return cdp.DispatchMouseEvent(input.Type, input.X, input.Y, input.Button, input.ClickCount, input.Modifiers)
	case "key":
		return cdp.DispatchKeyEvent(input.Type, input.Key, input.Code, input.Text, input.Modifiers)
	default:
		return fmt.Errorf("unknown input type: %s", input.InputType)
	}
}

// BrowserNavigate navigates the session's browser to a URL or executes a navigation action.
func (bs *BrowserService) BrowserNavigate(sessionID string, url string, action string) error {
	cdp, err := bs.requireCDP(sessionID)
	if err != nil {
		return err
	}

	if action != "" {
		switch action {
		case "back":
			return cdp.NavigateHistory(-1)
		case "forward":
			return cdp.NavigateHistory(1)
		default:
			return fmt.Errorf("unknown navigate action: %s", action)
		}
	}
	return cdp.Navigate(url)
}

var (
	errNoBrowser = fmt.Errorf("browser not running")
	errNoCDP     = fmt.Errorf("browser not connected")
)

// requireCDP returns the CDP client for a session's browser, or an error.
func (bs *BrowserService) requireCDP(sessionID string) (*browser.CDPClient, error) {
	inst := bs.browserMgr.Get(sessionID)
	if inst == nil {
		return nil, errNoBrowser
	}
	cdp := inst.CDP()
	if cdp == nil {
		return nil, errNoCDP
	}
	return cdp, nil
}

// BrowserInputParams holds the parameters for a browser input event.
type BrowserInputParams struct {
	InputType  string  `json:"inputType"` // "mouse" or "key"
	Type       string  `json:"type"`      // CDP event type
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button"`
	ClickCount int     `json:"clickCount"`
	Key        string  `json:"key"`
	Code       string  `json:"code"`
	Text       string  `json:"text"`
	Modifiers  int     `json:"modifiers"`
}

// BrowserRunning returns whether a browser with a CDP connection is running for the session.
// If running, also returns the current page URL (best-effort).
func (bs *BrowserService) BrowserRunning(sessionID string) (running bool, url string) {
	inst := bs.browserMgr.Get(sessionID)
	if inst == nil {
		return false, ""
	}
	cdp := inst.CDP()
	if cdp == nil {
		return false, ""
	}
	return true, cdp.GetCurrentURL()
}

// StopAll stops all browser instances. Called on server shutdown.
func (bs *BrowserService) StopAll() {
	bs.browserMgr.StopAll()
}
