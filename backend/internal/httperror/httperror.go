package httperror

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
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

func Forbidden(msg string) *Error {
	return &Error{Status: http.StatusForbidden, Message: msg}
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

// JSON writes data as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// RespondError classifies err into an HTTP status code and writes a JSON error response.
// 5xx errors are logged at Error level; 4xx at Warn.
func RespondError(w http.ResponseWriter, err error) {
	he := Classify(err)

	if he.Status >= 500 {
		slog.Error("http error", "status", he.Status, "error", he.Message, "cause", he.Cause)
	} else {
		slog.Warn("http error", "status", he.Status, "error", he.Message)
	}

	JSON(w, he.Status, map[string]string{"error": he.Message})
}
