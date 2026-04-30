// Package backup holds agentique-specific metadata helpers for backup files
// produced by agentkit/sqliteops. The retry/backup machinery itself lives in
// agentkit; only the schema-aware Metadata reader stays here.
package backup

import "database/sql"

// Metadata holds summary counts from a backup database.
type Metadata struct {
	Projects int64
	Sessions int64
	Events   int64
}

// BackupMetadata opens a backup DB read-only and returns row counts.
// Returns zero Metadata and an error if the DB cannot be read.
func BackupMetadata(path string) (Metadata, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return Metadata{}, err
	}
	defer db.Close()

	var m Metadata
	row := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM projects),
		(SELECT COUNT(*) FROM sessions),
		(SELECT COUNT(*) FROM session_events)`)
	if err := row.Scan(&m.Projects, &m.Sessions, &m.Events); err != nil {
		return Metadata{}, err
	}
	return m, nil
}
