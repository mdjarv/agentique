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
	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// ErrNoSSEStream is returned from GET /mcp. The Streamable-HTTP MCP spec
// requires clients to probe with GET to discover whether the server offers
// a server→client SSE channel; a 405 reply means "no SSE, POST only" and
// is part of the normal handshake on every session start. Log at debug so
// the probe doesn't generate warn-level noise.
var ErrNoSSEStream = httperror.MethodNotAllowed("no SSE stream at this endpoint").
	WithLogLevel(slog.LevelDebug)

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

// ServeHTTP implements http.Handler via httperror.HandlerFunc so the method
// can return typed errors.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	httperror.HandlerFunc(h.serve).ServeHTTP(w, r)
}

// serve dispatches on method. Streamable-HTTP transport: POST for JSON-RPC
// messages, GET returns ErrNoSSEStream (no server-initiated channel).
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) error {
	tok := bearer(r.Header.Get("Authorization"))
	sessionID, ok := h.tokens.Lookup(tok)
	if !ok {
		return httperror.Unauthorized("unauthorized")
	}

	switch r.Method {
	case http.MethodPost:
		return h.handlePost(w, r, sessionID)
	case http.MethodGet:
		return ErrNoSSEStream
	case http.MethodDelete:
		// Client signaling end of session. We tie lifetime to session destroy
		// elsewhere, so just accept.
		w.WriteHeader(http.StatusAccepted)
		return nil
	default:
		return httperror.MethodNotAllowed("method not allowed")
	}
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request, sessionID string) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return httperror.BadRequest("read body").WithCause(err)
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return httperror.BadRequest("invalid jsonrpc").WithCause(err)
	}

	// Notifications and responses (no id) per spec → 202, no body.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return nil
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
	return nil
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
	if len(h.dev.Slots()) == 0 {
		return toolError("No dev URL slots are configured on this server. Ask the operator to add [[dev-urls]] entries to agentique config.")
	}
	lease, err := h.dev.Acquire(sessionID)
	if err != nil {
		if errors.Is(err, devurls.ErrAllBusy) {
			holders := summarizeHolders(h.dev.Slots())
			return toolError("All dev URL slots are currently in use. Holders: " + holders)
		}
		return toolError("acquire failed: " + err.Error())
	}
	msg := fmt.Sprintf(
		"Acquired dev URL slot %q.\n"+
			"Public URL: %s (TLS-terminated by the reverse proxy)\n"+
			"Local port: %d\n"+
			"Public host: %s\n\n"+
			"Bind any HTTP service to 127.0.0.1:%d (or 0.0.0.0:%d) and it becomes reachable at the URL. Examples:\n"+
			"  - Vite dev server:  `just dev-frontend-remote %d %s` (Agentique) or `vite --port %d --host`\n"+
			"  - Go HTTP server:   pass `--addr :%d` or `http.ListenAndServe(\":%d\", ...)`\n"+
			"  - Any bind-to-port process works (static file servers, tunneled demos, etc.)\n\n"+
			"Release with ReleaseDevUrl when done (auto-released at session end).",
		lease.Slot, lease.URL, lease.Port, lease.PublicHost,
		lease.Port, lease.Port,
		lease.Port, lease.PublicHost, lease.Port,
		lease.Port, lease.Port,
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
			"description": "Lease a publicly-routable HTTPS URL that points at a local TCP port on this machine. Bind any HTTP service to the returned port and it becomes reachable at the returned URL (TLS terminated by the reverse proxy — valid certificate, so HTTPS-only features like passkeys/WebAuthn, secure cookies, and service workers work). Returns {slot, url, publicHost, port}. Idempotent — re-calling returns the existing lease for this session.",
			"inputSchema": emptySchema,
		},
		{
			"name":        ToolReleaseDev,
			"description": "Release any dev URL slot leased by this session. Idempotent — no-op if nothing is held. Slots also auto-release when the session ends.",
			"inputSchema": emptySchema,
		},
		{
			"name":        ToolListDevURLs,
			"description": "List all configured dev URL slots and their current holders (free or held by a specific session). Useful to check for contention before calling AcquireDevUrl.",
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
