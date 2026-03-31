package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperr"
	"github.com/mdjarv/agentique/backend/internal/respond"
)

// SSEEvent represents a server-sent event from the hub.
type SSEEvent struct {
	ProjectID string
	Type      string
	Payload   any
}

// SubscribeFunc returns an event channel and an unsubscribe function.
type SubscribeFunc func() (events <-chan SSEEvent, unsubscribe func())

// Handler handles HTTP REST requests for session operations.
type Handler struct {
	svc       *Service
	subscribe SubscribeFunc
}

// NewHandler creates a new session REST handler.
func NewHandler(svc *Service, subscribe SubscribeFunc) *Handler {
	return &Handler{svc: svc, subscribe: subscribe}
}

// HandleList returns sessions as JSON. Filters by ?project=<id> if provided.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project")

	var result ListSessionsResult
	var err error
	if projectID != "" {
		result, err = h.svc.ListSessions(r.Context(), projectID)
	} else {
		result, err = h.svc.ListAllSessions(r.Context())
	}
	if err != nil {
		respond.Error(w, httperr.Internal("list sessions", err))
		return
	}

	respond.JSON(w, http.StatusOK, result.Sessions)
}

// HandleGet returns a single session by ID.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	info, err := h.svc.GetSessionInfo(r.Context(), id)
	if err != nil {
		respond.Error(w, httperr.NotFound("session not found"))
		return
	}

	respond.JSON(w, http.StatusOK, info)
}

// HandleEvents streams session events as SSE.
// Filters by ?project=<id> if provided.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respond.Error(w, httperr.Internal("streaming not supported", nil))
		return
	}

	projectFilter := r.URL.Query().Get("project")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	events, unsubscribe := h.subscribe()
	defer unsubscribe()

	slog.Debug("sse client connected", "project_filter", projectFilter)
	defer slog.Debug("sse client disconnected")

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if projectFilter != "" && evt.ProjectID != projectFilter {
				continue
			}

			data, err := json.Marshal(map[string]any{
				"projectId": evt.ProjectID,
				"payload":   evt.Payload,
			})
			if err != nil {
				continue
			}

			fmt.Fprintf(w, "event: %s\n", evt.Type)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// HandleHistory returns the turn history for a session.
func (h *Handler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	result, err := h.svc.GetHistory(r.Context(), id)
	if err != nil {
		respond.Error(w, httperr.NotFound("session not found"))
		return
	}

	respond.JSON(w, http.StatusOK, result)
}

// HandleStop stops a running session.
func (h *Handler) HandleStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	if err := h.svc.StopSession(r.Context(), id); err != nil {
		respond.Error(w, err)
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// HandleQuery sends a prompt to a session.
func (h *Handler) HandleQuery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, httperr.BadRequest("invalid JSON body"))
		return
	}
	if body.Prompt == "" {
		respond.Error(w, httperr.BadRequest("prompt is required"))
		return
	}

	if err := h.svc.QuerySession(r.Context(), id, body.Prompt, nil); err != nil {
		respond.Error(w, err)
		return
	}

	respond.JSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// HandleDelete deletes a session and cleans up its worktree.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	if err := h.svc.DeleteSession(r.Context(), id); err != nil {
		respond.Error(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
