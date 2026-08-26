package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/janitor"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// usageTTL is how long a computed breakdown is reused before a request without
// ?refresh=1 triggers a fresh walk.
const usageTTL = 60 * time.Second

// Reclaimer removes a session's on-disk artifacts — worktree, Chrome profile,
// scratchpad — while keeping the session row and its git branch, so the session
// stays resumable. Implemented by session.Service, which owns the runtime
// registry and the git-aware worktree removal; this package stays a reporter and
// never reaches into either.
//
// The implementation re-plans from its own snapshot and intersects with the
// requested ids: a client that has been looking at a stale page can narrow the
// set but never widen it, and a session that woke up in the meantime comes back
// as a skip rather than a removal.
// It speaks the janitor's own vocabulary rather than this package's wire types,
// so the session package needs no dependency on the reporting package it is
// reported by.
type Reclaimer interface {
	ReclaimSessions(ctx context.Context, sessionIDs []string) (janitor.Result, []janitor.Skipped, error)
}

// Handler serves disk-usage endpoints. The expensive usage walk is cached and
// serialized behind a mutex so concurrent callers share a single computation.
type Handler struct {
	Queries *store.Queries
	// LiveIDs reports the sessions the runtime still holds. Optional; nil means
	// the verdicts rely on persisted state alone.
	LiveIDs func() map[string]bool
	// Probe answers the git questions behind the delete/reclaim verdicts.
	// Optional; nil leaves every verdict unestablished, which reads as unsafe.
	Probe SafetyProbe
	// Reclaim executes the reversible verb. Optional; nil makes the endpoint 501.
	Reclaim Reclaimer

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
	opt := UsageOptions{Probe: h.Probe}
	if h.LiveIDs != nil {
		opt.LiveIDs = h.LiveIDs()
	}
	usage, err := ComputeUsage(ctx, h.Queries, opt)
	if err != nil {
		return nil, err
	}
	h.last = usage
	h.lastAt = time.Now()
	return usage, nil
}

// invalidate drops the cached breakdown so the next usage request reflects
// whatever was just removed.
func (h *Handler) invalidate() {
	h.mu.Lock()
	h.last = nil
	h.mu.Unlock()
}

// maxReclaimRequest bounds the request body. The largest legitimate reclaim is
// every session on the machine, and a session id is 36 bytes of JSON plus
// quoting — a few hundred KiB is far past generous.
const maxReclaimRequest = 256 << 10

// HandleReclaim removes the on-disk artifacts of the named sessions, keeping
// each session row and git branch. This is the reversible verb: resume
// re-provisions the worktree from the branch, so nothing here needs the bar that
// guards Delete.
func (h *Handler) HandleReclaim(w http.ResponseWriter, r *http.Request) {
	if h.Reclaim == nil {
		httperror.RespondError(w, httperror.Internal("reclaim is unavailable", errors.New("no reclaimer configured")))
		return
	}
	var req ReclaimRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxReclaimRequest)).Decode(&req); err != nil {
		httperror.RespondError(w, httperror.BadRequest("invalid request body"))
		return
	}
	if len(req.SessionIDs) == 0 {
		httperror.RespondError(w, httperror.BadRequest("sessionIds is required"))
		return
	}
	res, skipped, err := h.Reclaim.ReclaimSessions(r.Context(), req.SessionIDs)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("reclaim sessions", err))
		return
	}
	h.invalidate()
	httperror.JSON(w, http.StatusOK, toReclaimResponse(res, skipped))
}

// toReclaimResponse maps the janitor's result onto the wire shape.
func toReclaimResponse(res janitor.Result, skipped []janitor.Skipped) ReclaimResponse {
	out := ReclaimResponse{
		Removed:    make([]ReclaimedArtifact, 0, len(res.Removed)),
		Skipped:    make([]ReclaimSkip, 0, len(skipped)),
		Failed:     make([]ReclaimSkip, 0, len(res.Failed)),
		FreedBytes: res.FreedBytes,
	}
	for _, it := range res.Removed {
		out.Removed = append(out.Removed, ReclaimedArtifact{
			Kind:      string(it.Kind),
			Path:      it.Path,
			SessionID: it.SessionID,
			Bytes:     it.SizeBytes,
		})
	}
	for _, s := range skipped {
		out.Skipped = append(out.Skipped, ReclaimSkip{SessionID: s.SessionID, Reason: s.Reason})
	}
	for _, f := range res.Failed {
		// The error text stays in the log; the client gets the path it applies to.
		out.Failed = append(out.Failed, ReclaimSkip{SessionID: f.Item.SessionID, Reason: f.Item.Path})
	}
	return out
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
	h.invalidate()
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
