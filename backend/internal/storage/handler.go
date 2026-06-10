package storage

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// usageTTL is how long a computed breakdown is reused before a request without
// ?refresh=1 triggers a fresh walk.
const usageTTL = 60 * time.Second

// Handler serves disk-usage endpoints. The expensive usage walk is cached and
// serialized behind a mutex so concurrent callers share a single computation.
type Handler struct {
	Queries *store.Queries

	mu     sync.Mutex
	last   *StorageUsage
	lastAt time.Time
}

// HandleDisk returns volume free/total stats. Cheap — safe to poll frequently.
func (h *Handler) HandleDisk(w http.ResponseWriter, r *http.Request) {
	stats, err := Stats()
	if err != nil {
		httperror.RespondError(w, httperror.Internal("read disk stats", err))
		return
	}
	httperror.JSON(w, http.StatusOK, stats)
}

// HandleUsage returns the full per-project / per-session breakdown, recomputing
// when ?refresh=1 is set or the cached result is stale.
func (h *Handler) HandleUsage(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	usage, err := h.usage(r.Context(), refresh)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("compute storage usage", err))
		return
	}
	httperror.JSON(w, http.StatusOK, usage)
}

func (h *Handler) usage(ctx context.Context, refresh bool) (*StorageUsage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !refresh && h.last != nil && time.Since(h.lastAt) < usageTTL {
		return h.last, nil
	}
	usage, err := ComputeUsage(ctx, h.Queries)
	if err != nil {
		return nil, err
	}
	h.last = usage
	h.lastAt = time.Now()
	return usage, nil
}

// HandleDeleteWorktree removes a single orphaned worktree directory. The path is
// validated to live strictly inside the worktrees root, at least two levels deep
// (a <bucket>/<session-dir>), so it can never target the root, a bucket, or any
// path outside the data directory.
func (h *Handler) HandleDeleteWorktree(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		httperror.RespondError(w, httperror.BadRequest("path is required"))
		return
	}
	target, err := safeWorktreePath(raw)
	if err != nil {
		httperror.RespondError(w, err)
		return
	}
	if err := os.RemoveAll(target); err != nil {
		httperror.RespondError(w, httperror.Internal("remove worktree", err))
		return
	}
	// Invalidate the cache so the next usage request reflects the freed space.
	h.mu.Lock()
	h.last = nil
	h.mu.Unlock()
	httperror.JSON(w, http.StatusOK, map[string]string{"removed": target})
}

// safeWorktreePath returns the cleaned absolute path only if it is strictly
// inside the worktrees root and at least two path segments below it.
func safeWorktreePath(raw string) (string, error) {
	root := filepath.Clean(paths.WorktreeDir())
	target := filepath.Clean(raw)
	if !filepath.IsAbs(target) {
		return "", httperror.BadRequest("path must be absolute")
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", httperror.Forbidden("path is outside the worktrees directory")
	}
	if len(strings.Split(rel, string(os.PathSeparator))) < 2 {
		return "", httperror.Forbidden("refusing to delete a worktree bucket root")
	}
	return target, nil
}
