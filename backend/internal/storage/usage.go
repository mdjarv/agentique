package storage

import (
	"context"
	"database/sql"
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

// UsageOptions carries what ComputeUsage cannot discover for itself: who is
// still live according to the runtime, and how to ask git about a branch.
//
// Both are optional. A nil Probe means the safety verdicts are not established
// — every session comes back unreclaimable and unsafe, which is the correct
// failure direction for a pair of verdicts that gate destructive actions.
type UsageOptions struct {
	// LiveIDs are the sessions the runtime still holds, whatever their persisted
	// state says. A session in here is never offered either verb.
	LiveIDs map[string]bool
	// Probe answers the git questions behind the verdicts.
	Probe SafetyProbe
}

// terminalStates mirrors janitor.IsTerminal — a session in one of these has
// stopped running, so its worktree is a candidate for either verb.
func isTerminal(state string) bool {
	return state == "done" || state == "stopped" || state == "failed"
}

// ComputeUsage walks the data directory and builds the full storage breakdown.
// This is expensive (a recursive walk of the worktrees tree, potentially many
// GB, plus up to two git calls per finished session) and is therefore cached and
// computed on demand by the handler.
func ComputeUsage(ctx context.Context, q *store.Queries, opt UsageOptions) (*StorageUsage, error) {
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
	refs := make([]sessionRef, 0, len(sessions))
	for _, s := range sessions {
		wt := ""
		if s.WorktreePath.Valid && s.WorktreePath.String != "" {
			wt = filepath.Clean(s.WorktreePath.String)
			byWorktree[wt] = s
		}
		refs = append(refs, sessionRef{ID: s.ID, WorktreePath: wt})
	}

	// Artifacts outside the data dir, and how many bytes each session owns there.
	tempArtifacts := discoverTempArtifacts(refs)
	tempBySession := make(map[string]int64, len(tempArtifacts))
	var tempBytes, chromeBytes, scratchBytes, foreignBytes int64
	var foreignCount int
	for _, a := range tempArtifacts {
		tempBytes += a.Bytes
		if a.SessionID != "" {
			tempBySession[a.SessionID] += a.Bytes
		}
		switch a.Kind {
		case TempKindChrome:
			chromeBytes += a.Bytes
		case TempKindScratchpad:
			scratchBytes += a.Bytes
		case TempKindForeignScratchpad:
			// Never in tempBySession: a foreign scratchpad belongs to no
			// session, so it can never be part of a reclaim's freed figure.
			foreignBytes += a.Bytes
			foreignCount++
		}
	}

	// Walk the worktrees tree two levels deep: <bucket>/<session-dir>. Summing
	// per-session dir sizes yields the worktrees category total in a single pass.
	projAgg := make(map[string]*ProjectStorage)
	orphans := make([]SessionStorage, 0)
	var worktreesBytes, reclaimableBytes int64
	var reclaimableCount int

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
					TotalBytes:   size,
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
			archivedAt := ""
			if sess.ArchivedAt.Valid {
				archivedAt = sess.ArchivedAt.String
			}

			verdicts := Verdicts{Safety: DeleteBlockedUnknown}
			if opt.Probe != nil {
				verdicts = Evaluate(opt.Probe, SafetyInput{
					Terminal:     isTerminal(sess.State),
					Live:         opt.LiveIDs[sess.ID],
					Merged:       sess.WorktreeMerged != 0,
					ProjectPath:  projectByID[sess.ProjectID].Path,
					Branch:       nullString(sess.WorktreeBranch),
					WorktreePath: wtPath,
				})
			}

			temp := tempBySession[sess.ID]
			total := size + temp
			if verdicts.Reclaimable {
				reclaimableBytes += total
				reclaimableCount++
			}

			agg.Sessions = append(agg.Sessions, SessionStorage{
				SessionID:    sess.ID,
				Name:         sess.Name,
				State:        sess.State,
				WorktreePath: wtPath,
				Bytes:        size,
				UpdatedAt:    sess.UpdatedAt,
				ArchivedAt:   archivedAt,
				Archived:     archivedAt != "",
				Merged:       sess.WorktreeMerged != 0,
				TempBytes:    temp,
				TotalBytes:   total,
				Reclaimable:  verdicts.Reclaimable,
				Safety:       verdicts.Safety,
				SafetyReason: verdicts.Safety.Reason(),
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

	tempCategories := []CategoryUsage{
		{Key: "chrome-profiles", Label: "Browser profiles", Bytes: chromeBytes},
		{Key: "scratchpads", Label: "Agent scratchpads", Bytes: scratchBytes},
		{Key: "foreign-scratchpads", Label: "Other Claude scratchpads", Bytes: foreignBytes},
	}

	return &StorageUsage{
		ComputedAt:         time.Now().UTC().Format(time.RFC3339),
		Disk:               disk,
		DataDirBytes:       dataDirBytes,
		Categories:         categories,
		Projects:           projectList,
		Orphans:            orphans,
		TempBytes:          tempBytes,
		TempCategories:     tempCategories,
		TempArtifacts:      tempArtifacts,
		ForeignScratchpads: foreignCount,
		Backups:            summarizeBackups(BackupDir()),
		ReclaimableBytes:   reclaimableBytes,
		ReclaimableCount:   reclaimableCount,
	}, nil
}

// nullString unwraps a nullable DB string to its value or "".
func nullString(s sql.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.String
}
