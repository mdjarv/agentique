package ws

import (
	"context"
	"log/slog"
)

// browserLaunchPrompt is injected into the session after the browser starts
// so the agent knows tools are now available.
const browserLaunchPrompt = `The browser has been launched and is visible to the user in a panel beside the chat. ` +
	`You can now interact with it using the ` + "`agentique-playwright`" + ` MCP tools ` +
	`(e.g. ` + "`mcp__agentique-playwright__browser_navigate`" + `, ` + "`mcp__agentique-playwright__browser_snapshot`" + `, ` +
	"`mcp__agentique-playwright__browser_click`" + `). ` +
	`The user can see everything you do in real time.`

func (c *conn) handleBrowserStatus(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p BrowserStatusPayload) (BrowserStatusResponse, error) {
		sessions := make(map[string]BrowserStatusSession, len(p.SessionIDs))
		for _, id := range p.SessionIDs {
			running, url := c.browserSvc.BrowserRunning(id)
			sessions[id] = BrowserStatusSession{Running: running, URL: url}
		}
		return BrowserStatusResponse{Sessions: sessions}, nil
	})
}

func (c *conn) handleBrowserLaunch(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p BrowserLaunchPayload) (struct{}, error) {
		if err := c.browserSvc.LaunchBrowser(p.SessionID); err != nil {
			return struct{}{}, err
		}

		// LaunchBrowser uses ReconnectMCPWait, so tools are ready by this point.
		// Use background context: the prompt must land even if the WS connection drops.
		if err := c.svc.EnqueueMessage(context.Background(), p.SessionID, browserLaunchPrompt, nil); err != nil {
			slog.Warn("browser launch prompt injection failed", "session_id", p.SessionID, "error", err)
		}

		return struct{}{}, nil
	})
}

func (c *conn) handleBrowserStop(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p BrowserStopPayload) (struct{}, error) {
		return struct{}{}, c.browserSvc.StopBrowser(p.SessionID)
	})
}

func (c *conn) handleBrowserInput(msg ClientMessage) {
	handleRequestQuiet(c, msg, func(_ context.Context, p BrowserInputPayload) (struct{}, error) {
		return struct{}{}, c.browserSvc.BrowserInput(p.SessionID, p.BrowserInputParams)
	})
}

func (c *conn) handleBrowserNavigate(msg ClientMessage) {
	handleRequest(c, msg, func(_ context.Context, p BrowserNavigatePayload) (struct{}, error) {
		return struct{}{}, c.browserSvc.BrowserNavigate(p.SessionID, p.URL, p.Action)
	})
}
