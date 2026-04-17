// Package mcphttp implements a Model Context Protocol Streamable-HTTP endpoint
// served from the same agentique HTTP server. It dispatches tool calls to
// in-process services (devurls, channel messaging) and authenticates each
// request via a per-session bearer token.
package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/devurls"
)

// Tool name constants for SendMessage interception parity. Other tools execute
// in-process via the handler's dispatcher.
const (
	ServerName       = "agentique"
	ToolSendMessage  = "SendMessage"
	ToolAcquireDev   = "AcquireDevUrl"
	ToolReleaseDev   = "ReleaseDevUrl"
	ToolListDevURLs  = "ListDevUrls"

	// SendMessageToolFullName is the MCP-prefixed name Claude uses when
	// invoking the tool. Permission interceptor keys on this string.
	SendMessageToolFullName = "mcp__" + ServerName + "__" + ToolSendMessage

	// Auto-approve interceptor names for dev URL tools.
	AcquireDevURLToolFullName = "mcp__" + ServerName + "__" + ToolAcquireDev
	ReleaseDevURLToolFullName = "mcp__" + ServerName + "__" + ToolReleaseDev
	ListDevURLsToolFullName   = "mcp__" + ServerName + "__" + ToolListDevURLs
)

// Handler serves the /mcp endpoint.
type Handler struct {
	tokens *TokenStore
	dev    *devurls.Store
}

// NewHandler returns a configured handler.
func NewHandler(tokens *TokenStore, dev *devurls.Store) *Handler {
	return &Handler{tokens: tokens, dev: dev}
}

// ServeHTTP implements http.Handler. Streamable-HTTP transport: POST for
// JSON-RPC messages, GET returns 405 (no server-initiated channel needed yet).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := bearer(r.Header.Get("Authorization"))
	sessionID, ok := h.tokens.Lookup(tok)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r, sessionID)
	case http.MethodGet:
		// SSE channel for server→client messages — not implemented; per spec
		// 405 indicates no SSE stream available at this endpoint.
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		// Optional: client signaling end of session. We tie lifetime to session
		// destroy elsewhere, so just accept.
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request, sessionID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid jsonrpc", http.StatusBadRequest)
		return
	}

	// Notifications and responses (no id) per spec → 202, no body.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := jsonrpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = mustJSON(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": ServerName, "version": "1.0.0"},
		})
	case "tools/list":
		resp.Result = mustJSON(map[string]any{"tools": h.toolDefinitions()})
	case "tools/call":
		result, rpcErr := h.dispatchTool(r.Context(), sessionID, req.Params)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = mustJSON(result)
		}
	default:
		resp.Error = &jsonrpcError{Code: -32601, Message: "method not found: " + req.Method}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Warn("mcphttp: write response", "error", err)
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *Handler) dispatchTool(_ context.Context, sessionID string, raw json.RawMessage) (toolResult, *jsonrpcError) {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &jsonrpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	switch p.Name {
	case ToolSendMessage:
		// Should never reach here — Claude's permission gate intercepts SendMessage
		// before it executes. Return a benign success to match mcp-channel behavior.
		return toolText("Message delivered."), nil
	case ToolAcquireDev:
		return h.handleAcquireDev(sessionID), nil
	case ToolReleaseDev:
		return h.handleReleaseDev(sessionID), nil
	case ToolListDevURLs:
		return h.handleListDevURLs(), nil
	default:
		return nil, &jsonrpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
}

func (h *Handler) handleAcquireDev(sessionID string) toolResult {
	lease, err := h.dev.Acquire(sessionID)
	if err != nil {
		if errors.Is(err, devurls.ErrAllBusy) {
			holders := summarizeHolders(h.dev.Slots())
			return toolError("All dev URL slots are currently in use. Holders: " + holders)
		}
		return toolError("acquire failed: " + err.Error())
	}
	msg := fmt.Sprintf(
		"Acquired dev URL slot %q.\nPublic URL: %s\nLocal port: %d\nPublic host: %s\n\nStart Vite with:\n  just dev-frontend-remote %d %s\n\nThe URL will be live as soon as Vite is listening on the local port. Call ReleaseDevUrl when done (or it will auto-release when this session ends).",
		lease.Slot, lease.URL, lease.Port, lease.PublicHost, lease.Port, lease.PublicHost,
	)
	return toolText(msg)
}

func (h *Handler) handleReleaseDev(sessionID string) toolResult {
	freed := h.dev.Release(sessionID)
	if len(freed) == 0 {
		return toolText("No dev URL slot was held by this session.")
	}
	return toolText("Released slot(s): " + strings.Join(freed, ", "))
}

func (h *Handler) handleListDevURLs() toolResult {
	infos := h.dev.Slots()
	if len(infos) == 0 {
		return toolText("No dev URL slots are configured.")
	}
	var lines []string
	for _, i := range infos {
		holder := i.HolderSessionID
		if holder == "" {
			holder = "(free)"
		}
		lines = append(lines, fmt.Sprintf("- %s → %s (port %d) — held by %s", i.Slot, i.URL, i.Port, holder))
	}
	return toolText("Dev URL slots:\n" + strings.Join(lines, "\n"))
}

// toolDefinitions describes each tool in MCP form.
func (h *Handler) toolDefinitions() []map[string]any {
	emptySchema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	return []map[string]any{
		{
			"name":        ToolSendMessage,
			"description": "Send a message to a teammate in this channel.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to": map[string]any{
						"type":        "string",
						"description": "Recipient: teammate name, or \"@spawn\" to create workers, or \"@dissolve\" to close the channel.",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Message content. For @spawn, a JSON string with channelName and workers array.",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"plan", "progress", "done", "message"},
						"description": "Message type for status signaling.",
					},
				},
				"required": []string{"to", "message"},
			},
		},
		{
			"name":        ToolAcquireDev,
			"description": "Lease a publicly-routable dev URL for this session's frontend dev server. Returns a URL, port, and a justfile command to start Vite. Idempotent — re-calling returns the existing lease.",
			"inputSchema": emptySchema,
		},
		{
			"name":        ToolReleaseDev,
			"description": "Release any dev URL slot leased by this session. Idempotent.",
			"inputSchema": emptySchema,
		},
		{
			"name":        ToolListDevURLs,
			"description": "List all configured dev URL slots and their current holders.",
			"inputSchema": emptySchema,
		},
	}
}

// --- transport types ---

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolResult map[string]any

func toolText(text string) toolResult {
	return toolResult{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
	}
}

func toolError(text string) toolResult {
	r := toolText(text)
	r["isError"] = true
	return r
}

// --- helpers ---

func bearer(h string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return h[len(prefix):]
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func summarizeHolders(infos []devurls.SlotInfo) string {
	holders := make([]string, 0, len(infos))
	for _, i := range infos {
		holder := i.HolderSessionID
		if holder == "" {
			holder = "(free)"
		}
		holders = append(holders, fmt.Sprintf("%s=%s", i.Slot, holder))
	}
	sort.Strings(holders)
	return strings.Join(holders, ", ")
}
