package auth

import (
	"encoding/json"
	"net/http"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// RegisterUserRoutes registers user preference endpoints on the given mux.
func (s *Service) RegisterUserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/user/preferences", s.handleUpdatePreferences)
}

type updatePreferencesRequest struct {
	SidebarFocusMode *bool `json:"sidebarFocusMode,omitempty"`
}

func (s *Service) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	session := UserFromContext(r.Context())
	if session == nil {
		httperror.RespondError(w, httperror.Unauthorized("not authenticated"))
		return
	}

	var req updatePreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid JSON body"))
		return
	}

	if req.SidebarFocusMode == nil {
		httperror.RespondError(w, httperror.BadRequest("no fields to update"))
		return
	}

	var focus int64
	if *req.SidebarFocusMode {
		focus = 1
	}
	if err := s.queries.UpdateUserSidebarFocusMode(r.Context(), store.UpdateUserSidebarFocusModeParams{
		SidebarFocusMode: focus,
		ID:               session.UserID,
	}); err != nil {
		httperror.RespondError(w, httperror.Internal("update preferences", err))
		return
	}

	httperror.JSON(w, http.StatusOK, map[string]any{
		"sidebarFocusMode": *req.SidebarFocusMode,
	})
}
