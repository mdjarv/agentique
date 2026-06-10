package storage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Stats returns volume statistics for the filesystem holding the data directory.
// Cheap (a single statfs syscall) — safe to poll frequently.
func Stats() (DiskStats, error) {
	dir := paths.DataDir()
	total, avail, used, err := diskStats(dir)
	if err != nil {
		return DiskStats{}, err
	}
	// df-style usage: percent of the user-accessible space (used + available),
	// which excludes root-reserved blocks — matches what `df` reports, rather
	// than used/total which inflates the figure by the reserved amount.
	var pct float64
	if denom := used + avail; denom > 0 {
		pct = float64(used) / float64(denom) * 100
	}
	return DiskStats{
		Path:         dir,
		TotalBytes:   total,
		FreeBytes:    avail,
		UsedBytes:    used,
		UsagePercent: pct,
	}, nil
}

// dirSize sums the size of every regular file under path. Unreadable entries
// are skipped rather than aborting the walk; symlinks are not followed.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// fileSize returns the size of a single file, or 0 if it does not exist.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// ComputeUsage walks the data directory and builds the full storage breakdown.
// This is expensive (a recursive walk of the worktrees tree, potentially many
// GB) and is therefore cached and computed on demand by the handler.
func ComputeUsage(ctx context.Context, q *store.Queries) (*StorageUsage, error) {
	dataDir := paths.DataDir()
	worktreeDir := paths.WorktreeDir()
	sessionFilesDir := paths.SessionFilesDir()

	disk, err := Stats()
	if err != nil {
		return nil, err
	}

	sessions, err := q.ListAllSessions(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := q.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	projectByID := make(map[string]store.Project, len(projects))
	for _, p := range projects {
		projectByID[p.ID] = p
	}
	// Cleaned worktree path -> session row.
	byWorktree := make(map[string]store.Session, len(sessions))
	for _, s := range sessions {
		if s.WorktreePath.Valid && s.WorktreePath.String != "" {
			byWorktree[filepath.Clean(s.WorktreePath.String)] = s
		}
	}

	// Walk the worktrees tree two levels deep: <bucket>/<session-dir>. Summing
	// per-session dir sizes yields the worktrees category total in a single pass.
	projAgg := make(map[string]*ProjectStorage)
	orphans := make([]SessionStorage, 0)
	var worktreesBytes int64

	buckets, _ := os.ReadDir(worktreeDir)
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		bucketPath := filepath.Join(worktreeDir, bucket.Name())
		sessionDirs, _ := os.ReadDir(bucketPath)
		for _, sd := range sessionDirs {
			if !sd.IsDir() {
				continue
			}
			wtPath := filepath.Join(bucketPath, sd.Name())
			size := dirSize(wtPath)
			worktreesBytes += size

			sess, known := byWorktree[filepath.Clean(wtPath)]
			if !known {
				orphans = append(orphans, SessionStorage{
					Name:         filepath.Join(bucket.Name(), sd.Name()),
					State:        "orphaned",
					WorktreePath: wtPath,
					Bytes:        size,
					Orphaned:     true,
				})
				continue
			}

			agg := projAgg[sess.ProjectID]
			if agg == nil {
				p := projectByID[sess.ProjectID]
				agg = &ProjectStorage{
					ProjectID: sess.ProjectID,
					Name:      p.Name,
					Slug:      p.Slug,
					Color:     p.Color,
					Icon:      p.Icon,
				}
				projAgg[sess.ProjectID] = agg
			}
			agg.TotalBytes += size
			agg.Sessions = append(agg.Sessions, SessionStorage{
				SessionID:    sess.ID,
				Name:         sess.Name,
				State:        sess.State,
				WorktreePath: wtPath,
				Bytes:        size,
				UpdatedAt:    sess.UpdatedAt,
			})
		}
	}

	projectList := make([]ProjectStorage, 0, len(projAgg))
	for _, p := range projAgg {
		sort.Slice(p.Sessions, func(i, j int) bool { return p.Sessions[i].Bytes > p.Sessions[j].Bytes })
		projectList = append(projectList, *p)
	}
	sort.Slice(projectList, func(i, j int) bool { return projectList[i].TotalBytes > projectList[j].TotalBytes })
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Bytes > orphans[j].Bytes })

	// Remaining categories. "other" is computed from the data-dir's top-level
	// entries that aren't a known category, avoiding a second full worktrees walk.
	sessionFilesBytes := dirSize(sessionFilesDir)
	backupsBytes := dirSize(filepath.Join(dataDir, "backups"))
	certsBytes := dirSize(filepath.Join(dataDir, "certs"))
	dbBytes := fileSize(paths.DBPath()) + fileSize(paths.DBPath()+"-wal") + fileSize(paths.DBPath()+"-shm")

	known := map[string]bool{"worktrees": true, "session-files": true, "backups": true, "certs": true}
	dbFiles := map[string]bool{"agentique.db": true, "agentique.db-wal": true, "agentique.db-shm": true}
	var otherBytes int64
	entries, _ := os.ReadDir(dataDir)
	for _, e := range entries {
		name := e.Name()
		if known[name] || dbFiles[name] {
			continue
		}
		full := filepath.Join(dataDir, name)
		if e.IsDir() {
			otherBytes += dirSize(full)
		} else {
			otherBytes += fileSize(full)
		}
	}

	categories := []CategoryUsage{
		{Key: "worktrees", Label: "Worktrees", Bytes: worktreesBytes},
		{Key: "backups", Label: "Backups", Bytes: backupsBytes},
		{Key: "database", Label: "Database", Bytes: dbBytes},
		{Key: "session-files", Label: "Session files", Bytes: sessionFilesBytes},
		{Key: "certs", Label: "Certificates", Bytes: certsBytes},
		{Key: "other", Label: "Other", Bytes: otherBytes},
	}
	dataDirBytes := worktreesBytes + sessionFilesBytes + backupsBytes + certsBytes + dbBytes + otherBytes

	return &StorageUsage{
		ComputedAt:   time.Now().UTC().Format(time.RFC3339),
		Disk:         disk,
		DataDirBytes: dataDirBytes,
		Categories:   categories,
		Projects:     projectList,
		Orphans:      orphans,
	}, nil
}
