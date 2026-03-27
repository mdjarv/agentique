package testmode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Handler provides test-only HTTP endpoints for hybrid E2E tests.
// Guarded by --test-mode; never mounted in production.
type Handler struct {
	Connector *Connector
	Manager   *session.Manager
	Queries   *store.Queries
	DB        *sql.DB
}

// RegisterRoutes mounts all test endpoints on the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/test/seed", h.HandleSeed)
	mux.HandleFunc("POST /api/test/inject-event", h.HandleInjectEvent)
	mux.HandleFunc("POST /api/test/reset", h.HandleReset)
	mux.HandleFunc("GET /api/test/state", h.HandleState)
}

// SeedRequest is the JSON body for POST /api/test/seed.
type SeedRequest struct {
	Projects []SeedProject `json:"projects"`
	Sessions []SeedSession `json:"sessions"`
}

// SeedProject defines a project to create during seeding.
type SeedProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Slug string `json:"slug"`
}

// SeedSession defines a session to create and optionally make live.
type SeedSession struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	Name      string     `json:"name"`
	WorkDir   string     `json:"workDir"`
	Live      bool       `json:"live"`      // if true, create via Manager (starts event loop)
	Behavior  []Scenario `json:"behavior"`  // scripted event sequences for mock
}

// SeedResult is returned from POST /api/test/seed.
type SeedResult struct {
	Projects int `json:"projects"`
	Sessions int `json:"sessions"`
}

// HandleSeed populates the DB with fixture data and optionally starts live sessions.
func (h *Handler) HandleSeed(w http.ResponseWriter, r *http.Request) {
	var req SeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	ctx := context.Background()

	for _, p := range req.Projects {
		if p.ID == "" {
			p.ID = uuid.NewString()
		}
		if p.Slug == "" {
			p.Slug = p.Name
		}
		if _, err := h.Queries.CreateProject(ctx, store.CreateProjectParams{
			ID:   p.ID,
			Name: p.Name,
			Path: p.Path,
			Slug: p.Slug,
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "create project %s: %v", p.ID, err)
			return
		}
	}

	for _, s := range req.Sessions {
		if s.ID == "" {
			s.ID = uuid.NewString()
		}

		// Pre-configure mock behavior before Connect() is called.
		if len(s.Behavior) > 0 {
			h.Connector.SetBehavior(s.ID, s.Behavior)
		}

		if s.Live {
			sess, err := h.Manager.Create(ctx, session.CreateParams{
				ID:        s.ID,
				ProjectID: s.ProjectID,
				Name:      s.Name,
				WorkDir:   s.WorkDir,
			})
			if err != nil {
				respondError(w, http.StatusInternalServerError, "create live session %s: %v", s.ID, err)
				return
			}
			// Associate the mock session with this Agentique session ID.
			h.Connector.Associate(sess.ID)
		} else {
			// DB-only session (not live).
			if _, err := h.Queries.CreateSession(ctx, store.CreateSessionParams{
				ID:             s.ID,
				ProjectID:      s.ProjectID,
				Name:           s.Name,
				WorkDir:        s.WorkDir,
				State:          "idle",
				Model:          "opus",
				PermissionMode: "default",
			}); err != nil {
				respondError(w, http.StatusInternalServerError, "create session %s: %v", s.ID, err)
				return
			}
		}
	}

	respondJSON(w, http.StatusOK, SeedResult{
		Projects: len(req.Projects),
		Sessions: len(req.Sessions),
	})
}

// InjectEventRequest is the JSON body for POST /api/test/inject-event.
type InjectEventRequest struct {
	SessionID string          `json:"sessionId"`
	Event     json.RawMessage `json:"event"`
}

// HandleInjectEvent pushes a single event into a mock session's channel.
func (h *Handler) HandleInjectEvent(w http.ResponseWriter, r *http.Request) {
	var req InjectEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	mockSess := h.Connector.Get(req.SessionID)
	if mockSess == nil {
		respondError(w, http.StatusNotFound, "no mock session for %s", req.SessionID)
		return
	}

	event, err := parseWireToClaudeEvent(req.Event)
	if err != nil {
		respondError(w, http.StatusBadRequest, "parse event: %v", err)
		return
	}

	if err := mockSess.InjectEvent(event); err != nil {
		respondError(w, http.StatusInternalServerError, "inject event: %v", err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleReset clears all data and mock state.
func (h *Handler) HandleReset(w http.ResponseWriter, r *http.Request) {
	// Close all live sessions first.
	h.Manager.CloseAll()
	h.Connector.Reset()

	ctx := context.Background()
	tables := []string{"session_events", "sessions", "projects"}
	for _, t := range tables {
		if _, err := h.DB.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			slog.Error("test reset: delete from "+t, "error", err)
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SessionState is a single session's state in the GET /api/test/state response.
type SessionState struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Live  bool   `json:"live"`
}

// HandleState returns current session states for test assertions.
func (h *Handler) HandleState(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	dbSessions, err := h.Queries.ListAllSessions(ctx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "list sessions: %v", err)
		return
	}

	states := make([]SessionState, len(dbSessions))
	for i, s := range dbSessions {
		states[i] = SessionState{
			ID:    s.ID,
			State: s.State,
			Live:  h.Manager.IsLive(s.ID),
		}
	}

	respondJSON(w, http.StatusOK, states)
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Warn("test endpoint error", "status", status, "error", msg)
	respondJSON(w, status, map[string]string{"error": msg})
}
