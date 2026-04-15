package session

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/mdjarv/agentique/backend/internal/browser"
)

// BrowserService handles browser lifecycle operations for sessions.
type BrowserService struct {
	mgr       *Manager
	browserMgr *browser.Manager
	hub       Broadcaster
}

// NewBrowserService creates a BrowserService.
func NewBrowserService(mgr *Manager, browserMgr *browser.Manager, hub Broadcaster) *BrowserService {
	return &BrowserService{mgr: mgr, browserMgr: browserMgr, hub: hub}
}

// PlaywrightMCPConfig returns the MCP config JSON string for Playwright
// pointing at the given CDP port.
// MCPServerName is the MCP server name used for Agentique's managed browser.
// Distinct from "playwright" to avoid collisions with globally-installed plugins.
const MCPServerName = "agentique-playwright"

func PlaywrightMCPConfig(port int) string {
	return fmt.Sprintf(`{"mcpServers":{%q:{"command":"npx","args":["@playwright/mcp","--cdp-endpoint","http://127.0.0.1:%d"]}}}`, MCPServerName, port)
}

// AllocatePort pre-allocates a browser port for a session. Called during
// session creation so the MCP config can reference the port before Chrome starts.
func (bs *BrowserService) AllocatePort(sessionID string) (int, error) {
	return bs.browserMgr.Port(sessionID)
}

// LaunchBrowser starts Chrome for the given session, connects CDP, starts
// screencast, and wires frame events to the WS hub.
func (bs *BrowserService) LaunchBrowser(sessionID string) error {
	sess := bs.mgr.Get(sessionID)
	if sess == nil {
		return ErrNotLive
	}

	port := sess.BrowserPort()
	var inst *browser.Instance
	var err error
	if port > 0 {
		inst, err = bs.browserMgr.LaunchOnPort(sessionID, port)
	} else {
		inst, err = bs.browserMgr.Launch(sessionID)
	}
	if err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}

	// Connect CDP client.
	cdpClient, err := browser.NewCDPClient(inst.CDPEndpoint)
	if err != nil {
		bs.browserMgr.Stop(sessionID)
		return fmt.Errorf("cdp connect: %w", err)
	}
	inst.SetCDP(cdpClient)

	// Wire screencast frames to WS hub.
	projectID := sess.ProjectID
	cdpClient.SetOnFrame(func(frame browser.FrameEvent) {
		bs.hub.Broadcast(projectID, "browser.frame", PushBrowserFrame{
			SessionID: sessionID, Data: frame.Data, Metadata: frame.Metadata,
		})
	})

	// Set a fixed viewport so content renders at a predictable size.
	if err := cdpClient.SetViewport(1280, 720); err != nil {
		slog.Warn("viewport override failed", "session_id", sessionID, "error", err)
	}

	// Start screencast: quality 80, match the viewport.
	if err := cdpClient.StartScreencast(80, 1280, 720); err != nil {
		bs.browserMgr.Stop(sessionID)
		return fmt.Errorf("screencast: %w", err)
	}

	// Tell Claude Code to reconnect the Playwright MCP server now that Chrome is up.
	// Uses the blocking variant so the caller knows tools are ready before
	// e.g. injecting a prompt that references them.
	// Non-fatal: MCP may not be configured yet if session was created before the
	// browser feature was enabled, or if the Playwright MCP server name differs.
	if err := sess.ReconnectMCPWait(MCPServerName, 10*time.Second); err != nil {
		slog.Info("mcp reconnect skipped", "session_id", sessionID, "error", err)
	}

	slog.Info("browser launched", "session_id", sessionID, "port", inst.Port)
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
		bs.hub.Broadcast(projectID, "browser.stopped", PushBrowserStopped{SessionID: sessionID, Reason: "user-initiated"})
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
