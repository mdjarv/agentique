package project

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

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

type updateProjectRequest struct {
	Slug string `json:"slug"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)
var validSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// Slugify converts a name to a URL-safe lowercase slug.
func Slugify(name string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "project"
	}
	return s
}

func (h *Handler) uniqueSlug(r *http.Request, base string) (string, error) {
	candidate := base
	for i := 2; i <= 100; i++ {
		_, err := h.Queries.GetProjectBySlug(r.Context(), candidate)
		if err != nil {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", fmt.Errorf("could not generate unique slug for %q", base)
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

	info, err := os.Stat(req.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, "path does not exist on disk")
		return
	}
	if !info.IsDir() {
		respondError(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	slug, err := h.uniqueSlug(r, Slugify(req.Name))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate slug")
		return
	}

	id := uuid.New().String()
	project, err := h.Queries.CreateProject(r.Context(), store.CreateProjectParams{
		ID:   id,
		Name: req.Name,
		Path: req.Path,
		Slug: slug,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	respondJSON(w, http.StatusCreated, project)
}

// HandleUpdate updates mutable project fields (currently: slug).
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "id is required")
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Slug == "" {
		respondError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if !validSlugRe.MatchString(req.Slug) {
		respondError(w, http.StatusBadRequest, "slug must be lowercase alphanumeric with dashes")
		return
	}

	// Check if slug is already taken by a different project.
	existing, err := h.Queries.GetProjectBySlug(r.Context(), req.Slug)
	if err == nil && existing.ID != id {
		respondError(w, http.StatusConflict, "slug is already in use")
		return
	}

	project, err := h.Queries.UpdateProjectSlug(r.Context(), store.UpdateProjectSlugParams{
		ID:   id,
		Slug: req.Slug,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update project")
		return
	}

	respondJSON(w, http.StatusOK, project)
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
