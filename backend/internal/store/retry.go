package store

import (
	"errors"
	"fmt"
	"time"

	"github.com/mdjarv/backoff"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// isTransientSQLiteError returns true for SQLITE_BUSY and SQLITE_LOCKED errors,
// which are transient lock-contention errors that may succeed on retry.
func isTransientSQLiteError(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	// Check primary result code (mask off extended bits).
	primary := code & 0xFF
	return primary == sqlite3.SQLITE_BUSY || primary == sqlite3.SQLITE_LOCKED
}

// RetryWrite wraps fn with exponential backoff, retrying only on transient
// SQLite errors (BUSY/LOCKED). Non-transient errors are returned immediately.
// Returns the original error (not backoff.ErrMaxAttemptsReached) on exhaustion.
func RetryWrite(fn func() error) error {
	var lastErr error
	err := backoff.Retry(func() error {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !isTransientSQLiteError(lastErr) {
			// Non-transient: abort retries immediately by returning nil
			// and letting the caller check lastErr.
			return nil
		}
		return lastErr // transient: signal backoff to retry
	},
		backoff.WithMinDuration(50*time.Millisecond),
		backoff.WithMaxDuration(2*time.Second),
		backoff.WithMaxAttempts(5),
	)
	if err != nil {
		// backoff.ErrMaxAttemptsReached — return the original SQLite error
		return fmt.Errorf("retry exhausted after transient SQLite errors: %w", lastErr)
	}
	return lastErr
}
