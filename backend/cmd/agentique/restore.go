package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/backup"
	"github.com/spf13/cobra"
)

var (
	restoreForce   bool
	restorePreOnly bool
)

// Safety copies of the live database, taken immediately before a restore
// overwrites it, live in their own filename namespace with their own quota.
//
// They deliberately sit OUTSIDE sqliteops' "agentique-pre-" namespace.
// sqliteops.Snapshot prunes that namespace by string prefix down to five
// entries on every server start, and "agentique-pre-restore-" is a prefix
// match — so nesting under it meant routine restarts and restore safety copies
// competed for the same five slots. Sitting outside it also keeps them clear of
// the periodic tiered prune, which skips any name whose timestamp does not
// parse.
const (
	restoreSafetyPrefix = "agentique-restore-"

	// legacyRestoreSafetyPrefix is the pre-rename name. Still recognised so
	// copies written by an older build get listed-out and pruned rather than
	// orphaned on disk forever.
	legacyRestoreSafetyPrefix = "agentique-pre-restore-"

	// restoreSafetyRetain caps the safety-copy pool. Each entry is a full-size
	// copy of the live database and only an explicit restore creates one.
	restoreSafetyRetain = 5

	restoreSafetyTimeLayout = "20060102-150405"
)

// isRestoreSafetyCopy reports whether name is a restore safety copy, under
// either the current or the legacy prefix.
func isRestoreSafetyCopy(name string) bool {
	_, ok := parseRestoreSafetyTime(name)
	return ok
}

// parseRestoreSafetyTime extracts the timestamp from a restore safety copy's
// filename. A name that merely starts with the prefix but carries an
// unparseable timestamp is not one of ours and is left alone.
func parseRestoreSafetyTime(name string) (time.Time, bool) {
	if !strings.HasSuffix(name, ".db") {
		return time.Time{}, false
	}
	for _, prefix := range []string{legacyRestoreSafetyPrefix, restoreSafetyPrefix} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		ts := strings.TrimSuffix(name[len(prefix):], ".db")
		t, err := time.Parse(restoreSafetyTimeLayout, ts)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}

// pruneRestoreSafetyCopies keeps the newest restoreSafetyRetain safety copies
// in dir and removes the rest. Ordering is by parsed timestamp, not filename,
// so the legacy and current prefixes interleave correctly.
//
// Pruning failures are reported but never abort a restore: the copy this call
// exists to protect has already been written.
func pruneRestoreSafetyCopies(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not prune restore safety copies: %v\n", err)
		return
	}

	type copyEntry struct {
		name string
		t    time.Time
	}
	var copies []copyEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t, ok := parseRestoreSafetyTime(e.Name())
		if !ok {
			continue
		}
		copies = append(copies, copyEntry{e.Name(), t})
	}

	if len(copies) <= restoreSafetyRetain {
		return
	}

	sort.Slice(copies, func(i, j int) bool { return copies[i].t.After(copies[j].t) })
	for _, c := range copies[restoreSafetyRetain:] {
		path := filepath.Join(dir, c.name)
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", c.name, err)
		}
	}
}

func init() {
	restoreCmd.Flags().BoolVarP(&restoreForce, "force", "f", false, "skip confirmation prompt")
	restoreCmd.Flags().BoolVar(&restorePreOnly, "pre", false, "show only pre-startup snapshots")
	rootCmd.AddCommand(restoreCmd)
}

var restoreCmd = &cobra.Command{
	Use:   "restore [backup-name-or-index]",
	Short: "List or restore database backups",
	Long: `Without arguments, lists available backups with metadata.
With an argument (1-based index or filename), restores that backup.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestore,
}

type backupEntry struct {
	name     string
	path     string
	size     int64
	isPreBkp bool
}

func runRestore(cmd *cobra.Command, args []string) error {
	dbFile := resolveDBPath()
	backupDir := filepath.Join(filepath.Dir(dbFile), "backups")

	entries, err := listBackups(backupDir)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No backups found.")
		return nil
	}

	if len(args) == 0 {
		return listMode(entries)
	}
	return restoreMode(entries, args[0], dbFile, backupDir)
}

func listBackups(dir string) ([]backupEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	var result []backupEntry
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}

		if isRestoreSafetyCopy(name) {
			continue // hide restore safety copies from listing
		}

		isPre := strings.HasPrefix(name, "agentique-pre-")
		isPeriodic := strings.HasPrefix(name, "agentique-") && !isPre

		if !isPre && !isPeriodic {
			continue
		}
		if restorePreOnly && !isPre {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		result = append(result, backupEntry{
			name:     name,
			path:     filepath.Join(dir, name),
			size:     info.Size(),
			isPreBkp: isPre,
		})
	}

	// Sort newest first (filenames are timestamped).
	sort.Slice(result, func(i, j int) bool {
		return result[i].name > result[j].name
	})

	return result, nil
}

func listMode(entries []backupEntry) error {
	fmt.Printf("%-4s  %-9s  %-20s  %8s  %8s  %8s  %8s\n",
		"#", "TYPE", "TIMESTAMP", "SIZE", "PROJECTS", "SESSIONS", "EVENTS")

	for i, e := range entries {
		typ := "periodic"
		if e.isPreBkp {
			typ = "pre"
		}

		ts := parseTimestampFromName(e.name)

		size := formatSize(e.size)

		m, err := backup.BackupMetadata(e.path)
		var projects, sessions, events string
		if err != nil {
			projects, sessions, events = "?", "?", "?"
		} else {
			projects = strconv.FormatInt(m.Projects, 10)
			sessions = strconv.FormatInt(m.Sessions, 10)
			events = strconv.FormatInt(m.Events, 10)
		}

		fmt.Printf("%-4d  %-9s  %-20s  %8s  %8s  %8s  %8s\n",
			i+1, typ, ts, size, projects, sessions, events)
	}
	return nil
}

func restoreMode(entries []backupEntry, arg string, dbFile string, backupDir string) error {
	entry, err := resolveBackupEntry(entries, arg)
	if err != nil {
		return err
	}

	// Check server not running.
	if isServerRunning() {
		return fmt.Errorf("server is running at %s — stop it first", addr)
	}

	// Show what we're about to do.
	m, metaErr := backup.BackupMetadata(entry.path)
	fmt.Printf("Restore: %s -> %s\n", entry.name, dbFile)
	if metaErr == nil {
		fmt.Printf("  %d projects, %d sessions, %d events\n", m.Projects, m.Sessions, m.Events)
	}

	if !restoreForce {
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Safety: backup current DB before overwriting.
	if _, err := os.Stat(dbFile); err == nil {
		safetyName := restoreSafetyPrefix + time.Now().UTC().Format(restoreSafetyTimeLayout) + ".db"
		safetyPath := filepath.Join(backupDir, safetyName)
		if err := copyFile(dbFile, safetyPath); err != nil {
			return fmt.Errorf("safety backup failed: %w", err)
		}
		fmt.Printf("Safety backup: %s\n", safetyName)
		pruneRestoreSafetyCopies(backupDir)
	}

	// Atomic restore: write to temp file, then rename.
	tmpPath := dbFile + ".restore-tmp"
	if err := copyFile(entry.path, tmpPath); err != nil {
		return fmt.Errorf("copy backup: %w", err)
	}
	if err := os.Rename(tmpPath, dbFile); err != nil {
		os.Remove(tmpPath)
		// On Windows a rename over an open file fails; the server is already
		// verified down, so this means another process holds the DB open.
		return fmt.Errorf("rename %s into place: %w (is another process holding the database open?)", dbFile, err)
	}

	// Remove stale WAL/SHM files from the old database.
	os.Remove(dbFile + "-wal")
	os.Remove(dbFile + "-shm")

	// Verify.
	m, err = backup.BackupMetadata(dbFile)
	if err != nil {
		fmt.Printf("Restored (could not verify: %v)\n", err)
	} else {
		fmt.Printf("Restored: %d projects, %d sessions, %d events\n", m.Projects, m.Sessions, m.Events)
	}

	return nil
}

func resolveBackupEntry(entries []backupEntry, arg string) (backupEntry, error) {
	// Try as 1-based index.
	if idx, err := strconv.Atoi(arg); err == nil {
		if idx < 1 || idx > len(entries) {
			return backupEntry{}, fmt.Errorf("index %d out of range (1-%d)", idx, len(entries))
		}
		return entries[idx-1], nil
	}

	// Try as filename or prefix match.
	var matches []backupEntry
	for _, e := range entries {
		if e.name == arg {
			return e, nil
		}
		if strings.HasPrefix(e.name, arg) {
			matches = append(matches, e)
		}
	}

	switch len(matches) {
	case 0:
		return backupEntry{}, fmt.Errorf("no backup matching %q", arg)
	case 1:
		return matches[0], nil
	default:
		return backupEntry{}, fmt.Errorf("ambiguous: %q matches %d backups", arg, len(matches))
	}
}

func isServerRunning() bool {
	client := apiClient()
	resp, err := client.Get(baseURL() + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func parseTimestampFromName(name string) string {
	// Strip known prefixes and .db suffix. Longest first, so the restore
	// namespaces are not swallowed by the prefixes they extend.
	ts := name
	for _, prefix := range []string{legacyRestoreSafetyPrefix, restoreSafetyPrefix, "agentique-pre-", "agentique-"} {
		if strings.HasPrefix(ts, prefix) {
			ts = ts[len(prefix):]
			break
		}
	}
	ts = strings.TrimSuffix(ts, ".db")

	t, err := time.Parse("20060102-150405", ts)
	if err != nil {
		return ts // return raw if unparseable
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
