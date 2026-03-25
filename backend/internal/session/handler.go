package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
		respondError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	respondJSON(w, http.StatusOK, result.Sessions)
}

// HandleGet returns a single session by ID.
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "id is required")
		return
	}

	info, err := h.svc.GetSessionInfo(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	respondJSON(w, http.StatusOK, info)
}

// HandleEvents streams session events as SSE.
// Filters by ?project=<id> if provided.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
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

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
