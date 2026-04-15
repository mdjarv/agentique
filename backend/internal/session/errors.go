package session

import "fmt"

// PersistError indicates a DB write failed after an in-memory operation
// succeeded. The in-memory state is correct for the current server lifetime
// but won't survive a restart.
//
// Callers can check with errors.As:
//
//	var pe *PersistError
//	if errors.As(err, &pe) { /* non-fatal, warn the user */ }
type PersistError struct {
	Op  string // operation that failed, e.g. "update session model"
	Err error  // underlying DB error
}

func (e *PersistError) Error() string {
	return fmt.Sprintf("persist %s: %v", e.Op, e.Err)
}

func (e *PersistError) Unwrap() error {
	return e.Err
}

func newPersistError(op string, err error) *PersistError {
	return &PersistError{Op: op, Err: err}
}
