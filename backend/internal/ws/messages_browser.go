package ws

import (
	"errors"

	"github.com/mdjarv/agentique/backend/internal/session"
)

// BrowserLaunchPayload is the request to start a Chrome browser for a session.
type BrowserLaunchPayload struct {
	SessionID string `json:"sessionId"`
}

func (p *BrowserLaunchPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

// BrowserStopPayload is the request to stop a session's browser.
type BrowserStopPayload struct {
	SessionID string `json:"sessionId"`
}

func (p *BrowserStopPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

// BrowserInputPayload forwards a mouse or keyboard event to the browser.
type BrowserInputPayload struct {
	SessionID string `json:"sessionId"`
	session.BrowserInputParams
}

func (p *BrowserInputPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

// BrowserStatusPayload queries running browser state for one or more sessions.
type BrowserStatusPayload struct {
	SessionIDs []string `json:"sessionIds"`
}

func (p *BrowserStatusPayload) Validate() error {
	if len(p.SessionIDs) == 0 {
		return errors.New("sessionIds is required")
	}
	return nil
}

// BrowserStatusSession describes the browser state for a single session.
type BrowserStatusSession struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
}

// BrowserStatusResponse is the response to a browser.status request.
type BrowserStatusResponse struct {
	Sessions map[string]BrowserStatusSession `json:"sessions"`
}

// BrowserNavigatePayload navigates the browser to a URL or performs a history action.
type BrowserNavigatePayload struct {
	SessionID string `json:"sessionId"`
	URL       string `json:"url,omitempty"`
	Action    string `json:"action,omitempty"` // "back" or "forward"
}

func (p *BrowserNavigatePayload) Validate() error {
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	if p.URL == "" && p.Action == "" {
		return errors.New("url or action is required")
	}
	return nil
}
