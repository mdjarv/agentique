package store

import (
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// RunMigrations runs all pending database migrations using goose.
// The caller must provide an fs.FS containing the migration files at
// the "migrations" subdirectory.
func RunMigrations(db *sql.DB, migrationsFS fs.FS) error {
	goose.SetBaseFS(migrationsFS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}

	return goose.Up(db, "migrations")
}
