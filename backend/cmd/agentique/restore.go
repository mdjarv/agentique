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

	"github.com/allbin/agentique/backend/internal/backup"
	"github.com/spf13/cobra"
)

var (
	restoreForce   bool
	restorePreOnly bool
)

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

		isPre := strings.HasPrefix(name, "agentique-pre-")
		isPeriodic := strings.HasPrefix(name, "agentique-") && !isPre
		isPreRestore := strings.HasPrefix(name, "agentique-pre-restore-")

		if !isPre && !isPeriodic {
			continue
		}
		if isPreRestore {
			continue // hide pre-restore safety copies from listing
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
		safetyName := fmt.Sprintf("agentique-pre-restore-%s.db", time.Now().UTC().Format("20060102-150405"))
		safetyPath := filepath.Join(backupDir, safetyName)
		if err := copyFile(dbFile, safetyPath); err != nil {
			return fmt.Errorf("safety backup failed: %w", err)
		}
		fmt.Printf("Safety backup: %s\n", safetyName)
	}

	// Atomic restore: write to temp file, then rename.
	tmpPath := dbFile + ".restore-tmp"
	if err := copyFile(entry.path, tmpPath); err != nil {
		return fmt.Errorf("copy backup: %w", err)
	}
	if err := os.Rename(tmpPath, dbFile); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
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
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(baseURL() + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func parseTimestampFromName(name string) string {
	// Strip known prefixes and .db suffix.
	ts := name
	for _, prefix := range []string{"agentique-pre-", "agentique-"} {
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
