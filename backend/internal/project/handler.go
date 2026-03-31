package project

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/allbin/agentique/backend/internal/httperr"
	"github.com/allbin/agentique/backend/internal/respond"
	"github.com/allbin/agentique/backend/internal/session"
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
	Name            *string                 `json:"name,omitempty"`
	Slug            *string                 `json:"slug,omitempty"`
	BehaviorPresets *session.BehaviorPresets `json:"behaviorPresets,omitempty"`
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

// HandleList returns all projects as a JSON array.
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	projects, err := h.Queries.ListProjects(r.Context())
	if err != nil {
		respond.Error(w, httperr.Internal("list projects", err))
		return
	}
	respond.JSON(w, http.StatusOK, projects)
}

// HandleCreate creates a new project from the JSON request body.
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, httperr.BadRequest("invalid JSON body"))
		return
	}

	if req.Name == "" {
		respond.Error(w, httperr.BadRequest("name is required"))
		return
	}
	if req.Path == "" {
		respond.Error(w, httperr.BadRequest("path is required"))
		return
	}

	cleanPath := filepath.Clean(req.Path)
	if !filepath.IsAbs(cleanPath) {
		respond.Error(w, httperr.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		parentInfo, parentErr := os.Stat(filepath.Dir(cleanPath))
		if parentErr != nil || !parentInfo.IsDir() {
			respond.Error(w, httperr.BadRequest("parent directory does not exist"))
			return
		}
		if err := os.MkdirAll(cleanPath, 0o755); err != nil {
			respond.Error(w, httperr.Internal("create directory", err))
			return
		}
		if out, err := exec.Command("git", "init", cleanPath).CombinedOutput(); err != nil {
			respond.Error(w, httperr.Internal(fmt.Sprintf("git init failed: %s", out), err))
			return
		}
	} else if err != nil {
		respond.Error(w, httperr.BadRequest("cannot access path"))
		return
	} else if !info.IsDir() {
		respond.Error(w, httperr.BadRequest("path is not a directory"))
		return
	}
	req.Path = cleanPath

	slug, err := h.uniqueSlug(r, Slugify(req.Name))
	if err != nil {
		respond.Error(w, httperr.Internal("generate slug", err))
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
		// Classify handles SQLITE_CONSTRAINT_UNIQUE → 409.
		respond.Error(w, err)
		return
	}

	respond.JSON(w, http.StatusCreated, project)
}

// HandleUpdate updates mutable project fields (slug, behaviorPresets).
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, httperr.BadRequest("invalid JSON body"))
		return
	}

	if req.Name == nil && req.Slug == nil && req.BehaviorPresets == nil {
		respond.Error(w, httperr.BadRequest("no fields to update"))
		return
	}

	project, err := h.Queries.GetProject(r.Context(), id)
	if err != nil {
		respond.Error(w, httperr.NotFound("project not found"))
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			respond.Error(w, httperr.BadRequest("name must not be empty"))
			return
		}

		// Use explicit slug if provided, otherwise derive from new name.
		var slug string
		if req.Slug != nil {
			slug = *req.Slug
		} else {
			slug = Slugify(name)
			slug, err = h.uniqueSlug(r, slug)
			if err != nil {
				respond.Error(w, httperr.Internal("generate slug", err))
				return
			}
		}

		if !validSlugRe.MatchString(slug) {
			respond.Error(w, httperr.BadRequest("slug must be lowercase alphanumeric with dashes"))
			return
		}
		existing, slugErr := h.Queries.GetProjectBySlug(r.Context(), slug)
		if slugErr == nil && existing.ID != id {
			respond.Error(w, httperr.Conflict("slug is already in use"))
			return
		}

		project, err = h.Queries.UpdateProjectName(r.Context(), store.UpdateProjectNameParams{
			ID:   id,
			Name: name,
			Slug: slug,
		})
		if err != nil {
			respond.Error(w, httperr.Internal("update name", err))
			return
		}
	} else if req.Slug != nil {
		slug := *req.Slug
		if !validSlugRe.MatchString(slug) {
			respond.Error(w, httperr.BadRequest("slug must be lowercase alphanumeric with dashes"))
			return
		}
		existing, slugErr := h.Queries.GetProjectBySlug(r.Context(), slug)
		if slugErr == nil && existing.ID != id {
			respond.Error(w, httperr.Conflict("slug is already in use"))
			return
		}
		project, err = h.Queries.UpdateProjectSlug(r.Context(), store.UpdateProjectSlugParams{
			ID:   id,
			Slug: slug,
		})
		if err != nil {
			respond.Error(w, httperr.Internal("update slug", err))
			return
		}
	}

	if req.BehaviorPresets != nil {
		project, err = h.Queries.UpdateProjectBehaviorPresets(r.Context(), store.UpdateProjectBehaviorPresetsParams{
			ID:                     id,
			DefaultBehaviorPresets: req.BehaviorPresets.String(),
		})
		if err != nil {
			respond.Error(w, httperr.Internal("update behavior presets", err))
			return
		}
	}

	respond.JSON(w, http.StatusOK, project)
}

// HandleListPresetDefinitions returns the curated preset definitions.
func (h *Handler) HandleListPresetDefinitions(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, session.PresetRegistry)
}

// HandleDelete deletes a project by its ID extracted from the URL path.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respond.Error(w, httperr.BadRequest("id is required"))
		return
	}

	if err := h.Queries.DeleteProject(r.Context(), id); err != nil {
		respond.Error(w, httperr.Internal("delete project", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
