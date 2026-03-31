package testmode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperr"
	"github.com/mdjarv/agentique/backend/internal/respond"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
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
	mux.HandleFunc("GET /api/test/export-session/{id}", h.HandleExportSession)
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
	ID              string     `json:"id"`
	ProjectID       string     `json:"projectId"`
	Name            string     `json:"name"`
	WorkDir         string     `json:"workDir"`
	Live            bool       `json:"live"`             // if true, create via Manager (starts event loop)
	Behavior        []Scenario `json:"behavior"`         // scripted event sequences for mock
	PlanMode        bool       `json:"planMode"`         // start in plan permission mode
	AutoApproveMode string     `json:"autoApproveMode"`  // "manual" (default), "auto", "fullAuto"
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
		respond.Error(w, httperr.BadRequest(fmt.Sprintf("invalid JSON: %v", err)))
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
			respond.Error(w, httperr.Internal(fmt.Sprintf("create project %s", p.ID), err))
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
				ID:              s.ID,
				ProjectID:       s.ProjectID,
				Name:            s.Name,
				WorkDir:         s.WorkDir,
				PlanMode:        s.PlanMode,
				AutoApproveMode: s.AutoApproveMode,
			})
			if err != nil {
				respond.Error(w, httperr.Internal(fmt.Sprintf("create live session %s", s.ID), err))
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
				respond.Error(w, httperr.Internal(fmt.Sprintf("create session %s", s.ID), err))
				return
			}
		}
	}

	respond.JSON(w, http.StatusOK, SeedResult{
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
		respond.Error(w, httperr.BadRequest(fmt.Sprintf("invalid JSON: %v", err)))
		return
	}

	mockSess := h.Connector.Get(req.SessionID)
	if mockSess == nil {
		respond.Error(w, httperr.NotFound(fmt.Sprintf("no mock session for %s", req.SessionID)))
		return
	}

	event, err := parseWireToClaudeEvent(req.Event)
	if err != nil {
		respond.Error(w, httperr.BadRequest(fmt.Sprintf("parse event: %v", err)))
		return
	}

	if err := mockSess.InjectEvent(event); err != nil {
		respond.Error(w, httperr.Internal("inject event", err))
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleReset clears all data and mock state.
func (h *Handler) HandleReset(w http.ResponseWriter, r *http.Request) {
	// Close all live sessions first.
	h.Manager.CloseAll()
	h.Connector.Reset()

	ctx := context.Background()
	tables := []string{"session_events", "sessions", "teams", "projects"}
	for _, t := range tables {
		if _, err := h.DB.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			slog.Error("test reset: delete from "+t, "error", err)
		}
	}

	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		respond.Error(w, httperr.Internal("list sessions", err))
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

	respond.JSON(w, http.StatusOK, states)
}

// ExportedSession is the JSON format for a recorded session fixture.
type ExportedSession struct {
	Metadata ExportMetadata `json:"metadata"`
	Turns    []ExportTurn   `json:"turns"`
}

// ExportMetadata contains session and project info for the fixture.
type ExportMetadata struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	ProjectName string `json:"projectName"`
	ProjectPath string `json:"projectPath"`
	Model       string `json:"model"`
	CapturedAt  string `json:"capturedAt"`
}

// ExportTurn is a single turn (prompt + events with timing).
type ExportTurn struct {
	Prompt   string   `json:"prompt"`
	Scenario Scenario `json:"scenario"`
}

// HandleExportSession reads a session's events from DB and returns them
// as a fixture in the Scenario/ScriptedEvent format for Playwright replay.
func (h *Handler) HandleExportSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	ctx := context.Background()

	dbSess, err := h.Queries.GetSession(ctx, sessionID)
	if err != nil {
		respond.Error(w, httperr.NotFound(fmt.Sprintf("session %s not found", sessionID)))
		return
	}

	project, err := h.Queries.GetProject(ctx, dbSess.ProjectID)
	if err != nil {
		respond.Error(w, httperr.Internal("get project", err))
		return
	}

	events, err := h.Queries.ListEventsBySession(ctx, sessionID)
	if err != nil {
		respond.Error(w, httperr.Internal("list events", err))
		return
	}

	scrubber := NewScrubber(dbSess.WorkDir, os.Getenv("HOME"))

	// Group events by turn, extracting prompts and computing relative timing.
	type turnData struct {
		prompt string
		events []ScriptedEvent
		start  time.Time
	}
	turnMap := make(map[int64]*turnData)
	var turnOrder []int64

	for _, ev := range events {
		td, ok := turnMap[ev.TurnIndex]
		if !ok {
			td = &turnData{}
			turnMap[ev.TurnIndex] = td
			turnOrder = append(turnOrder, ev.TurnIndex)
		}

		if ev.Type == "prompt" {
			// Extract prompt text from the JSON data.
			var p struct {
				Prompt string `json:"prompt"`
			}
			if json.Unmarshal([]byte(ev.Data), &p) == nil {
				td.prompt = p.Prompt
			}
			continue
		}

		// Parse created_at for timing delta.
		ts, tsErr := time.Parse("2006-01-02T15:04:05.000", ev.CreatedAt)
		if tsErr != nil {
			ts = time.Time{} // fallback: no timing
		}

		var delayMs int
		if !ts.IsZero() {
			if td.start.IsZero() {
				td.start = ts
			}
			delayMs = int(ts.Sub(td.start).Milliseconds())
		}

		// Build wire event: merge type into the data JSON.
		data := session.NormalizeEventJSON(ev.Type, []byte(ev.Data))
		var m map[string]any
		if json.Unmarshal(data, &m) == nil {
			m["type"] = ev.Type
			scrubbed, _ := json.Marshal(m)
			td.events = append(td.events, ScriptedEvent{
				Delay: delayMs,
				Event: json.RawMessage(scrubber.Scrub(string(scrubbed))),
			})
		}
	}

	// Assemble export.
	turns := make([]ExportTurn, 0, len(turnOrder))
	for _, idx := range turnOrder {
		td := turnMap[idx]
		turns = append(turns, ExportTurn{
			Prompt: scrubber.Scrub(td.prompt),
			Scenario: Scenario{
				Events: td.events,
			},
		})
	}

	export := ExportedSession{
		Metadata: ExportMetadata{
			SessionID:   sessionID,
			SessionName: dbSess.Name,
			ProjectName: project.Name,
			ProjectPath: scrubber.Scrub(project.Path),
			Model:       dbSess.Model,
			CapturedAt:  time.Now().UTC().Format(time.RFC3339),
		},
		Turns: turns,
	}

	respond.JSON(w, http.StatusOK, export)
}
