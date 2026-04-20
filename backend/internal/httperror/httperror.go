// Package httperror provides HTTP-aware error types, a classifier that maps
// arbitrary errors to status codes, and a handler adapter so handlers can
// return errors instead of writing responses manually. A single choke point
// (RespondError) owns status-code selection, response body encoding, and
// leveled logging — callers just return a typed error.
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

// Error is an HTTP-aware error carrying the status code, a user-facing
// message, an optional wrapped cause, and an optional log-level override.
// When LogLevel is nil the responder picks a level from the status code
// (5xx → Error, 4xx → Warn, else Info).
type Error struct {
	Status   int
	Message  string
	Cause    error
	LogLevel *slog.Level
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// WithLogLevel returns a copy of e with an explicit log level. Use this for
// expected non-2xx responses that shouldn't pollute logs at warn level —
// e.g. protocol handshake probes.
func (e *Error) WithLogLevel(l slog.Level) *Error {
	copy := *e
	copy.LogLevel = &l
	return &copy
}

// WithCause returns a copy of e with an attached cause for Unwrap / errors.Is
// chains. The message is not modified so user-facing text stays clean.
func (e *Error) WithCause(cause error) *Error {
	copy := *e
	copy.Cause = cause
	return &copy
}

func BadRequest(msg string) *Error {
	return &Error{Status: http.StatusBadRequest, Message: msg}
}

func Unauthorized(msg string) *Error {
	return &Error{Status: http.StatusUnauthorized, Message: msg}
}

func Forbidden(msg string) *Error {
	return &Error{Status: http.StatusForbidden, Message: msg}
}

func NotFound(msg string) *Error {
	return &Error{Status: http.StatusNotFound, Message: msg}
}

func MethodNotAllowed(msg string) *Error {
	return &Error{Status: http.StatusMethodNotAllowed, Message: msg}
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

// RespondError classifies err, logs at the appropriate level, and writes a
// JSON error response. 5xx default to Error level, 4xx to Warn; errors can
// override via WithLogLevel for expected non-2xx (e.g. protocol probes).
func RespondError(w http.ResponseWriter, err error) {
	he := Classify(err)

	level := defaultLevel(he.Status)
	if he.LogLevel != nil {
		level = *he.LogLevel
	}

	slog.Log(nil, level, "http error",
		"status", he.Status,
		"error", he.Message,
		"cause", he.Cause,
	)

	JSON(w, he.Status, map[string]string{"error": he.Message})
}

func defaultLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// HandlerFunc is an http.Handler whose function signature returns an error.
// Non-nil errors are handed to RespondError; nil means the handler already
// wrote its response.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP implements http.Handler.
func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := f(w, r); err != nil {
		RespondError(w, err)
	}
}
