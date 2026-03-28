package httperr

import (
	"database/sql"
	"errors"
	"net/http"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Error is an HTTP-aware error that carries a status code and user-facing message.
type Error struct {
	Status  int
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func BadRequest(msg string) *Error {
	return &Error{Status: http.StatusBadRequest, Message: msg}
}

func NotFound(msg string) *Error {
	return &Error{Status: http.StatusNotFound, Message: msg}
}

func Conflict(msg string) *Error {
	return &Error{Status: http.StatusConflict, Message: msg}
}

func Internal(msg string, cause error) *Error {
	return &Error{Status: http.StatusInternalServerError, Message: msg, Cause: cause}
}

// Classify converts an arbitrary error into an *Error with an appropriate
// HTTP status code. Known error types are mapped to specific codes;
// unrecognized errors become 500 Internal.
func Classify(err error) *Error {
	var he *Error
	if errors.As(err, &he) {
		return he
	}

	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return Conflict("resource already exists")
	}

	if errors.Is(err, sql.ErrNoRows) {
		return NotFound("not found")
	}

	return Internal(err.Error(), err)
}
