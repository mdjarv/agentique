package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// RunInTx runs fn inside a database transaction. If fn returns an error or
// panics, the transaction is rolled back. Otherwise it is committed.
func RunInTx(db *sql.DB, fn func(q *Queries) error) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op after commit

	if err := fn(New(db).WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

// Open opens a SQLite database at the given path with WAL mode and foreign keys enabled.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// SQLite PRAGMAs are per-connection. Go's connection pool creates new
	// connections that won't inherit them. A single connection avoids this —
	// SQLite serializes writes anyway so there's no throughput loss.
	db.SetMaxOpenConns(1)

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

	// Retry on lock contention for up to 5s instead of failing immediately.
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}

	// NORMAL is safe with WAL and skips a redundant fsync per transaction.
	if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		db.Close()
		return nil, err
	}

	// This file holds auth session tokens, every paired machine's bearer token,
	// and pairing/invite tokens in plaintext. SQLite creates it (and the WAL it
	// just enabled) under the process umask, which is typically 0644 — readable
	// by every other local user. Restricting it here rather than at one call
	// site means every path that opens the DB gets it, including --db pointing
	// somewhere outside the data directory. Best-effort: a database we cannot
	// chmod (a read-only mount, a foreign owner) is still a usable database.
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := paths.SecureFile(p); err != nil {
			slog.Warn("could not restrict database file permissions", "path", p, "error", err)
		}
	}

	return db, nil
}
