package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Open opens a SQLite database at the given path with WAL mode and foreign keys enabled.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}

	// Enable foreign key constraint enforcement.
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
