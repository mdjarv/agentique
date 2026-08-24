// Package storage reports disk usage for the Agentique data directory: free
// space on the volume, a category breakdown, and per-project / per-session
// worktree footprints used to find and reclaim lingering session worktrees.
package storage

// DiskStats describes the filesystem volume holding the data directory.
type DiskStats struct {
	Path         string  `json:"path"`
	TotalBytes   uint64  `json:"totalBytes"`
	FreeBytes    uint64  `json:"freeBytes"`
	UsedBytes    uint64  `json:"usedBytes"`
	UsagePercent float64 `json:"usagePercent"`
}

// CategoryUsage is the on-disk size of one top-level data-dir category.
type CategoryUsage struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Bytes int64  `json:"bytes"`
}

// SessionStorage is the disk footprint of a single worktree. For a live
// session SessionID/Name/State/UpdatedAt are populated; for an orphan (a
// worktree dir with no matching session row) Orphaned is true and Name carries
// the on-disk "<bucket>/<dir>" label.
//
// Archived mirrors the sidebar's Archived section (a non-empty archived_at) so
// the disk view can label a session the way the sidebar does.
//
// Merged is what the bulk sweep keys on, and the distinction matters: archiving
// is a one-click tidy gesture (including a whole-shelf "Archive all"), while
// this page's delete removes worktrees AND branches AND rows. Only a merged
// session is safe to sweep in bulk — its commits are already on main, so there
// is nothing left to lose.
type SessionStorage struct {
	SessionID    string `json:"sessionId"`
	Name         string `json:"name"`
	State        string `json:"state"`
	WorktreePath string `json:"worktreePath"`
	Bytes        int64  `json:"bytes"`
	UpdatedAt    string `json:"updatedAt"`
	ArchivedAt   string `json:"archivedAt"`
	Archived     bool   `json:"archived"`
	Merged       bool   `json:"merged"`
	Orphaned     bool   `json:"orphaned"`
}

// ProjectStorage groups live-session worktree footprints under a project.
type ProjectStorage struct {
	ProjectID  string           `json:"projectId"`
	Name       string           `json:"name"`
	Slug       string           `json:"slug"`
	Color      string           `json:"color"`
	Icon       string           `json:"icon"`
	TotalBytes int64            `json:"totalBytes"`
	Sessions   []SessionStorage `json:"sessions"`
}

// StorageUsage is the full breakdown returned by GET /api/storage/usage.
type StorageUsage struct {
	ComputedAt   string           `json:"computedAt"`
	Disk         DiskStats        `json:"disk"`
	DataDirBytes int64            `json:"dataDirBytes"`
	Categories   []CategoryUsage  `json:"categories"`
	Projects     []ProjectStorage `json:"projects"`
	Orphans      []SessionStorage `json:"orphans"`
}
