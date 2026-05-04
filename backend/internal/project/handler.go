package project

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/allbin/agentkit/worktree"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
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
	Color           *string                 `json:"color,omitempty"`
	Icon            *string                 `json:"icon,omitempty"`
	Folder          *string                 `json:"folder,omitempty"`
	MaxSessions     *int64                  `json:"maxSessions,omitempty"`
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

func (h *Handler) uniqueSlug(r *http.Request, base, excludeID string) (string, error) {
	candidate := base
	for i := 2; i <= 100; i++ {
		existing, err := h.Queries.GetProjectBySlug(r.Context(), candidate)
		if err != nil {
			return candidate, nil
		}
		if excludeID != "" && existing.ID == excludeID {
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
		httperror.RespondError(w, httperror.Internal("list projects", err))
		return
	}
	httperror.JSON(w, http.StatusOK, projects)
}

// HandleCreate creates a new project from the JSON request body.
func (h *Handler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid JSON body"))
		return
	}

	if req.Name == "" {
		httperror.RespondError(w, httperror.BadRequest("name is required"))
		return
	}
	if req.Path == "" {
		httperror.RespondError(w, httperror.BadRequest("path is required"))
		return
	}

	cleanPath := filepath.Clean(req.Path)
	if !filepath.IsAbs(cleanPath) {
		httperror.RespondError(w, httperror.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		parentInfo, parentErr := os.Stat(filepath.Dir(cleanPath))
		if parentErr != nil || !parentInfo.IsDir() {
			httperror.RespondError(w, httperror.BadRequest("parent directory does not exist"))
			return
		}
		if err := os.MkdirAll(cleanPath, 0o755); err != nil {
			httperror.RespondError(w, httperror.Internal("create directory", err))
			return
		}
		if out, err := exec.Command("git", "init", cleanPath).CombinedOutput(); err != nil {
			httperror.RespondError(w, httperror.Internal(fmt.Sprintf("git init failed: %s", out), err))
			return
		}
	} else if err != nil {
		httperror.RespondError(w, httperror.BadRequest("cannot access path"))
		return
	} else if !info.IsDir() {
		httperror.RespondError(w, httperror.BadRequest("path is not a directory"))
		return
	}
	req.Path = cleanPath

	slug, err := h.uniqueSlug(r, Slugify(req.Name), "")
	if err != nil {
		httperror.RespondError(w, httperror.Internal("generate slug", err))
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
		httperror.RespondError(w, err)
		return
	}

	httperror.JSON(w, http.StatusCreated, project)
}

// HandleUpdate updates mutable project fields (slug, behaviorPresets).
func (h *Handler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httperror.RespondError(w, httperror.BadRequest("id is required"))
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid JSON body"))
		return
	}

	if req.Name == nil && req.Slug == nil && req.BehaviorPresets == nil && req.Color == nil && req.Icon == nil && req.Folder == nil && req.MaxSessions == nil {
		httperror.RespondError(w, httperror.BadRequest("no fields to update"))
		return
	}

	project, err := h.Queries.GetProject(r.Context(), id)
	if err != nil {
		httperror.RespondError(w, httperror.NotFound("project not found"))
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			httperror.RespondError(w, httperror.BadRequest("name must not be empty"))
			return
		}

		// Use explicit slug if provided, otherwise derive from new name.
		var slug string
		if req.Slug != nil {
			slug = *req.Slug
		} else {
			slug = Slugify(name)
			slug, err = h.uniqueSlug(r, slug, id)
			if err != nil {
				httperror.RespondError(w, httperror.Internal("generate slug", err))
				return
			}
		}

		if !validSlugRe.MatchString(slug) {
			httperror.RespondError(w, httperror.BadRequest("slug must be lowercase alphanumeric with dashes"))
			return
		}
		existing, slugErr := h.Queries.GetProjectBySlug(r.Context(), slug)
		if slugErr == nil && existing.ID != id {
			httperror.RespondError(w, httperror.Conflict("slug is already in use"))
			return
		}

		project, err = h.Queries.UpdateProjectName(r.Context(), store.UpdateProjectNameParams{
			ID:   id,
			Name: name,
			Slug: slug,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update name", err))
			return
		}
	} else if req.Slug != nil {
		slug := *req.Slug
		if !validSlugRe.MatchString(slug) {
			httperror.RespondError(w, httperror.BadRequest("slug must be lowercase alphanumeric with dashes"))
			return
		}
		existing, slugErr := h.Queries.GetProjectBySlug(r.Context(), slug)
		if slugErr == nil && existing.ID != id {
			httperror.RespondError(w, httperror.Conflict("slug is already in use"))
			return
		}
		project, err = h.Queries.UpdateProjectSlug(r.Context(), store.UpdateProjectSlugParams{
			ID:   id,
			Slug: slug,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update slug", err))
			return
		}
	}

	if req.BehaviorPresets != nil {
		project, err = h.Queries.UpdateProjectBehaviorPresets(r.Context(), store.UpdateProjectBehaviorPresetsParams{
			ID:                     id,
			DefaultBehaviorPresets: req.BehaviorPresets.String(),
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update behavior presets", err))
			return
		}
	}

	if req.Color != nil {
		project, err = h.Queries.UpdateProjectColor(r.Context(), store.UpdateProjectColorParams{
			ID:    id,
			Color: *req.Color,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update color", err))
			return
		}
	}

	if req.Icon != nil {
		project, err = h.Queries.UpdateProjectIcon(r.Context(), store.UpdateProjectIconParams{
			ID:   id,
			Icon: *req.Icon,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update icon", err))
			return
		}
	}

	if req.Folder != nil {
		project, err = h.Queries.UpdateProjectFolder(r.Context(), store.UpdateProjectFolderParams{
			ID:     id,
			Folder: *req.Folder,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update folder", err))
			return
		}
	}

	if req.MaxSessions != nil {
		if *req.MaxSessions < 0 {
			httperror.RespondError(w, httperror.BadRequest("maxSessions must be non-negative"))
			return
		}
		project, err = h.Queries.UpdateProjectMaxSessions(r.Context(), store.UpdateProjectMaxSessionsParams{
			ID:          id,
			MaxSessions: *req.MaxSessions,
		})
		if err != nil {
			httperror.RespondError(w, httperror.Internal("update max sessions", err))
			return
		}
	}

	httperror.JSON(w, http.StatusOK, project)
}

// HandleListPresetDefinitions returns the curated preset definitions.
func (h *Handler) HandleListPresetDefinitions(w http.ResponseWriter, r *http.Request) {
	httperror.JSON(w, http.StatusOK, session.PresetRegistry)
}

// HandleDelete deletes a project by its ID extracted from the URL path.
// Cleans up worktrees, branches, and session files before removing the DB row.
func (h *Handler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httperror.RespondError(w, httperror.BadRequest("id is required"))
		return
	}

	ctx := r.Context()

	// Get project path for git operations.
	project, err := h.Queries.GetProject(ctx, id)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("get project", err))
		return
	}

	// Clean up session resources before cascading DB delete.
	sessions, err := h.Queries.ListSessionsByProject(ctx, id)
	if err != nil {
		slog.Warn("list sessions for cleanup failed", "project_id", id, "error", err)
	} else {
		for _, sess := range sessions {
			h.cleanupSessionResources(ctx, project.Path, sess)
		}
	}

	if err := h.Queries.DeleteProject(ctx, id); err != nil {
		httperror.RespondError(w, httperror.Internal("delete project", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// cleanupSessionResources removes worktree, branches, and session files for a
// single session. Best-effort: errors are logged but never block deletion.
func (h *Handler) cleanupSessionResources(ctx context.Context, projectPath string, sess store.Session) {
	branch := ""
	if sess.WorktreeBranch.Valid {
		branch = sess.WorktreeBranch.String
	}
	if wtPath := sess.WorktreePath; wtPath.Valid && wtPath.String != "" {
		removeWorktreeBestEffort(ctx, projectPath, branch, wtPath.String)
	}
	if branch != "" {
		if err := gitops.DeleteBranch(projectPath, branch); err != nil {
			slog.Warn("branch delete failed during project cleanup",
				"session_id", sess.ID, "error", err)
		}
		gitops.DeleteRemoteBranch(projectPath, branch)
	}

	filesDir := filepath.Join(paths.SessionFilesDir(), sess.ID)
	if err := os.RemoveAll(filesDir); err != nil {
		slog.Warn("session files cleanup failed during project delete",
			"session_id", sess.ID, "error", err)
	}
}

// removeWorktreeBestEffort tears down a worktree via agentkit, falling back to
// a direct directory removal if adoption fails. Mirrors session.realWorktreeOps.
func removeWorktreeBestEffort(ctx context.Context, projectPath, branch, wtPath string) {
	if _, err := os.Stat(wtPath); err != nil {
		return
	}
	repo, err := worktree.NewLocalRepo(projectPath)
	if err != nil {
		slog.Warn("worktree remove: NewLocalRepo failed", "project", projectPath, "error", err)
		return
	}
	ws, err := repo.Worktree(ctx, worktree.WorktreeSpec{
		Path:   wtPath,
		Branch: worktree.SanitizeBranch(branch),
	})
	if err != nil {
		slog.Warn("worktree remove: adopt failed, removing directly", "path", wtPath, "error", err)
		_ = os.RemoveAll(wtPath)
		return
	}
	if err := ws.Close(ctx); err != nil {
		slog.Warn("worktree close failed", "path", wtPath, "error", err)
	}
}
