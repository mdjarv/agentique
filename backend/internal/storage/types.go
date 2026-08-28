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

// TempArtifact is an agentique-owned directory outside the data directory: a
// per-session Chrome profile or Claude scratchpad under the OS temp dir.
// SessionID is empty when the artifact maps to no session row — an orphan.
type TempArtifact struct {
	Kind      string `json:"kind"` // TempKindChrome | TempKindScratchpad
	Path      string `json:"path"`
	SessionID string `json:"sessionId"`
	Bytes     int64  `json:"bytes"`
}

// SessionStorage is the disk footprint of a single worktree. For a live
// session SessionID/Name/State/UpdatedAt are populated; for an orphan (a
// worktree dir with no matching session row) Orphaned is true and Name carries
// the on-disk "<bucket>/<dir>" label.
//
// Archived mirrors the sidebar's Archived section (a non-empty archived_at) so
// the disk view can label a session the way the sidebar does. It is a filing
// gesture and says nothing about safety — archiving a session with two unmerged
// commits is exactly as ordinary as archiving a finished one.
//
// The page offers two verbs against a session, and they answer to different
// bars:
//
//   - Reclaim removes the checked-out tree, the Chrome profile and the
//     scratchpad, keeping the row and the git branch; resume re-provisions from
//     the branch. Reversible, so Reclaimable asks only that the session is not
//     live and not dirty.
//   - Delete removes the row, the branch and the tree. Irreversible, so Safety
//     has to establish that the commits already exist on the project's main
//     line. Merged (`worktree_merged`) is one input to that and no longer the
//     definition — see safety.go.
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

	// TempBytes is the Chrome profile plus scratchpad this session owns under
	// the OS temp dir; TotalBytes is Bytes + TempBytes, which is what a reclaim
	// of this session actually frees.
	TempBytes  int64 `json:"tempBytes"`
	TotalBytes int64 `json:"totalBytes"`

	// Reclaimable reports whether the reversible verb is offered.
	Reclaimable bool `json:"reclaimable"`
	// Safety is the delete verdict; SafetyReason is its human phrasing, empty
	// when safe. Both are absent from a peer that predates them, and a client
	// must read a missing Safety as "not established" rather than as safe.
	Safety       DeleteSafety `json:"safety,omitempty"`
	SafetyReason string       `json:"safetyReason,omitempty"`
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
//
// DataDirBytes and TempBytes are two separate totals on purpose: the first is
// what lives under paths.DataDir(), the second what agentique put in the OS temp
// dir. Summing them into one figure would make "Agentique data" mean something
// other than the directory the volume line names.
type StorageUsage struct {
	ComputedAt   string           `json:"computedAt"`
	Disk         DiskStats        `json:"disk"`
	DataDirBytes int64            `json:"dataDirBytes"`
	Categories   []CategoryUsage  `json:"categories"`
	Projects     []ProjectStorage `json:"projects"`
	Orphans      []SessionStorage `json:"orphans"`

	// TempBytes totals TempArtifacts; TempCategories groups them by kind for the
	// breakdown bar. Both optional, so an older peer's payload still parses.
	TempBytes      int64           `json:"tempBytes,omitempty"`
	TempCategories []CategoryUsage `json:"tempCategories,omitempty"`
	TempArtifacts  []TempArtifact  `json:"tempArtifacts,omitempty"`
	// ForeignScratchpads counts the TempKindForeignScratchpad directories —
	// Claude scratchpads under the same root belonging to checkouts agentique
	// does not manage. Reported so the page can stop under-stating the disk;
	// never attributed to a session and never part of a reclaim.
	ForeignScratchpads int `json:"foreignScratchpads,omitempty"`
	// Backups describes the backup directory's two namespaces. Nil when there
	// are none, or when backups are disabled.
	Backups *BackupSummary `json:"backups,omitempty"`
	// ReclaimableBytes is what reclaiming every reclaimable session would free.
	ReclaimableBytes int64 `json:"reclaimableBytes,omitempty"`
	// ReclaimableCount is how many sessions that is.
	ReclaimableCount int `json:"reclaimableCount,omitempty"`
}

// BackupSummary splits the backup directory the way a trim has to think about
// it: periodic backups are churn a trim may remove, pre-migration snapshots are
// deliberate safety copies it never touches.
type BackupSummary struct {
	PeriodicCount int   `json:"periodicCount,omitempty"`
	PeriodicBytes int64 `json:"periodicBytes,omitempty"`
	SnapshotCount int   `json:"snapshotCount,omitempty"`
	SnapshotBytes int64 `json:"snapshotBytes,omitempty"`
	// OldestPeriodic is the "YYYYMMDD-HHMMSS" stamp of the oldest periodic
	// backup — how far back point-in-time recovery currently reaches.
	OldestPeriodic string `json:"oldestPeriodic,omitempty"`
	// Trimmable is how many periodic backups the default trim would remove, so
	// the page can offer the verb only when it would do something.
	Trimmable int `json:"trimmable,omitempty"`
}

// TrimBackupsRequest is the body of POST /api/storage/backups/trim. Keep is
// clamped up server-side; a client cannot ask for an empty backup directory.
type TrimBackupsRequest struct {
	Keep int `json:"keep"`
}

// TrimBackupsResponse reports what was actually removed.
type TrimBackupsResponse struct {
	Removed    []string `json:"removed"`
	FreedBytes int64    `json:"freedBytes"`
	Kept       int      `json:"kept"`
}

// ReclaimRequest is the body of POST /api/storage/reclaim. The server re-plans
// from its own snapshot and intersects with these ids, so a stale client can
// narrow the set but never widen it.
type ReclaimRequest struct {
	SessionIDs []string `json:"sessionIds"`
}

// ReclaimedArtifact is one directory a reclaim actually removed.
type ReclaimedArtifact struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	SessionID string `json:"sessionId"`
	Bytes     int64  `json:"bytes"`
}

// ReclaimSkip records a requested session the server declined to reclaim, and
// why. A skip is a normal outcome, not an error: the usual cause is that the
// session woke up between the page's last refresh and the click.
type ReclaimSkip struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// ReclaimResponse is the result of POST /api/storage/reclaim.
type ReclaimResponse struct {
	Removed    []ReclaimedArtifact `json:"removed"`
	Skipped    []ReclaimSkip       `json:"skipped"`
	Failed     []ReclaimSkip       `json:"failed"`
	FreedBytes int64               `json:"freedBytes"`
}
