package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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
	Slug            *string                  `json:"slug,omitempty"`
	BehaviorPresets *session.BehaviorPresets  `json:"behaviorPresets,omitempty"`
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

func respondError(w http.ResponseWriter, status int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Error("http error", "status", status, "error", msg)
	respondJSON(w, status, map[string]string{"error": msg})
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

	cleanPath := filepath.Clean(req.Path)
	if !filepath.IsAbs(cleanPath) {
		respondError(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	info, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		parentInfo, parentErr := os.Stat(filepath.Dir(cleanPath))
		if parentErr != nil || !parentInfo.IsDir() {
			respondError(w, http.StatusBadRequest, "parent directory does not exist")
			return
		}
		if err := os.MkdirAll(cleanPath, 0o755); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to create directory: %v", err)
			return
		}
		if out, err := exec.Command("git", "init", cleanPath).CombinedOutput(); err != nil {
			respondError(w, http.StatusInternalServerError, "git init failed: %s", out)
			return
		}
	} else if err != nil {
		respondError(w, http.StatusBadRequest, "cannot access path")
		return
	} else if !info.IsDir() {
		respondError(w, http.StatusBadRequest, "path is not a directory")
		return
	}
	req.Path = cleanPath

	slug, err := h.uniqueSlug(r, Slugify(req.Name))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate slug: %v", err)
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
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			respondError(w, http.StatusConflict, "project with this path already exists")
			return
		}
		respondError(w, http.StatusInternalServerError, "create project: %v", err)
		return
	}

	respondJSON(w, http.StatusCreated, project)
}

// HandleUpdate updates mutable project fields (slug, behaviorPresets).
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

	if req.Slug == nil && req.BehaviorPresets == nil {
		respondError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	// Start with current project state.
	project, err := h.Queries.GetProject(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}

	if req.Slug != nil {
		slug := *req.Slug
		if !validSlugRe.MatchString(slug) {
			respondError(w, http.StatusBadRequest, "slug must be lowercase alphanumeric with dashes")
			return
		}
		existing, slugErr := h.Queries.GetProjectBySlug(r.Context(), slug)
		if slugErr == nil && existing.ID != id {
			respondError(w, http.StatusConflict, "slug is already in use")
			return
		}
		project, err = h.Queries.UpdateProjectSlug(r.Context(), store.UpdateProjectSlugParams{
			ID:   id,
			Slug: slug,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "update slug: %v", err)
			return
		}
	}

	if req.BehaviorPresets != nil {
		project, err = h.Queries.UpdateProjectBehaviorPresets(r.Context(), store.UpdateProjectBehaviorPresetsParams{
			ID:                     id,
			DefaultBehaviorPresets: req.BehaviorPresets.String(),
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "update behavior presets: %v", err)
			return
		}
	}

	respondJSON(w, http.StatusOK, project)
}

// HandleListPresetDefinitions returns the curated preset definitions.
func (h *Handler) HandleListPresetDefinitions(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, session.PresetRegistry)
}

// HandleDelete deletes a project by its ID extracted from the URL path.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := h.Queries.DeleteProject(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "delete project: %v", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
