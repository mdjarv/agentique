package ws

import (
	"context"
	"log/slog"
)

// browserLaunchPrompt is injected into the session when the user opens the panel
// so the agent knows it is now being watched. The agentique-playwright tools
// were already available — the panel just makes the same browser visible.
const browserLaunchPrompt = `The user has opened the browser panel beside the chat. Your ` + "`agentique-playwright`" + ` browser is now visible to them in real time, and they can watch and intervene as you work. Keep using the tools as normal.`

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

		// The agent's browser tools are always available; this prompt just tells it
		// the panel is now open. Background context: the prompt must land even if
		// the WS connection drops.
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
