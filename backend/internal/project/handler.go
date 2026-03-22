package project

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Handler handles HTTP requests for project CRUD operations.
type Handler struct {
	Queries *store.Queries
}

type createProjectRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// HandleList returns all projects as a JSON array.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Queries.ListProjects(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	respondJSON(w, http.StatusOK, projects)
}

// HandleCreate creates a new project from the JSON request body.
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Path == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Validate that the path exists on disk.
	if _, err := os.Stat(req.Path); err != nil {
		respondError(w, http.StatusBadRequest, "path does not exist on disk")
		return
	}

	id := uuid.New().String()
	project, err := h.Queries.CreateProject(r.Context(), store.CreateProjectParams{
		ID:   id,
		Name: req.Name,
		Path: req.Path,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	respondJSON(w, http.StatusCreated, project)
}

// HandleDelete deletes a project by its ID extracted from the URL path.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := h.Queries.DeleteProject(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
