package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// ContentSource finds a session's served bytes: a file the agent wrote to its
// session files directory, and an image the history builder detached from a
// persisted event. [LocalContent] answers for this machine's sessions; the
// server's router puts a paired machine's behind the same interface, so the
// routes that serve either never learn which it was.
type ContentSource interface {
	SessionFile(ctx context.Context, sessionID, rel string) (content.Item, error)
	EventImage(ctx context.Context, sessionID string, eventID int64, idx int) (content.Item, error)
}

// LocalContent is [ContentSource] over this machine's disk and database.
type LocalContent struct {
	// Queries reads event rows; only EventImage needs it.
	Queries *store.Queries
	// FilesDir is the session files root; empty means paths.SessionFilesDir(),
	// resolved per call so a test's AGENTIQUE_HOME applies.
	FilesDir string
}

// SessionFile opens rel inside the session's files directory.
func (c LocalContent) SessionFile(_ context.Context, sessionID, rel string) (content.Item, error) {
	// The id becomes a path component, so it is validated for what it is
	// before any join; see the {id} note in CLAUDE.md.
	if uuid.Validate(sessionID) != nil {
		return content.Item{}, content.ErrInvalidPath
	}
	dir := c.FilesDir
	if dir == "" {
		dir = paths.SessionFilesDir()
	}
	return content.OpenInRoot(filepath.Join(dir, sessionID), rel, 0)
}

// EventImage decodes the idx-th inline image of one of the session's events.
func (c LocalContent) EventImage(ctx context.Context, sessionID string, eventID int64, idx int) (content.Item, error) {
	if c.Queries == nil {
		return content.Item{}, errors.New("event images need a database")
	}
	// The session id is part of the key, so an event of another session is
	// not found rather than served.
	row, err := c.Queries.GetSessionEvent(ctx, store.GetSessionEventParams{ID: eventID, SessionID: sessionID})
	if err != nil {
		return content.Item{}, content.ErrNotFound
	}
	dataURL, ok := inlineImageAt(row.Type, row.Data, idx)
	if !ok {
		return content.Item{}, content.ErrNotFound
	}
	mediaType, body, err := decodeDataURL(dataURL)
	if err != nil {
		return content.Item{}, content.ErrNotFound
	}
	// An event row is never rewritten, so the bytes behind the URL never change.
	return content.FromBytes(EventImageName(eventID, idx, mediaType), body, true), nil
}

// imageExtensions names the raster types an event image may render as. The
// allowlist that decides is still httpsecurity's, read through the name; this
// only turns a media type into the extension that allowlist reads. A type not
// here gets no extension, and so is a download.
var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/avif": ".avif",
	"image/bmp":  ".bmp",
}

// EventImageName is the name an event image is served under.
func EventImageName(eventID int64, idx int, mediaType string) string {
	return fmt.Sprintf("event-%d-%d%s", eventID, idx, imageExtensions[mediaType])
}

// ContentHandler serves a [ContentSource] on the session content routes.
type ContentHandler struct {
	Source ContentSource
}

// HandleFile answers GET /api/sessions/{id}/files/{filepath...}.
func (h *ContentHandler) HandleFile(w http.ResponseWriter, r *http.Request) {
	// A {id} wildcard is NOT one path segment: ServeMux unescapes the
	// capture, so %2F arrives here as a real separator.
	sessionID := r.PathValue("id")
	if uuid.Validate(sessionID) != nil {
		httperror.RespondError(w, httperror.BadRequest("session id must be a UUID"))
		return
	}
	rel := r.PathValue("filepath")
	if rel == "" {
		httperror.RespondError(w, httperror.BadRequest("file path is required"))
		return
	}
	item, err := h.Source.SessionFile(r.Context(), sessionID, rel)
	if err != nil {
		content.RespondError(w, err)
		return
	}
	content.Serve(w, r, item)
}

// HandleEventImage answers GET /api/sessions/{id}/events/{eventId}/images/{idx}.
func (h *ContentHandler) HandleEventImage(w http.ResponseWriter, r *http.Request) {
	sessionID, eventID, idx, herr := ParseEventImage(r)
	if herr != nil {
		httperror.RespondError(w, herr)
		return
	}
	item, err := h.Source.EventImage(r.Context(), sessionID, eventID, idx)
	if err != nil {
		content.RespondError(w, err)
		return
	}
	content.Serve(w, r, item)
}

// ParseEventImage validates an event image route's three path parameters for
// what they are before any of them reaches a query.
func ParseEventImage(r *http.Request) (sessionID string, eventID int64, idx int, herr *httperror.Error) {
	sessionID = r.PathValue("id")
	if uuid.Validate(sessionID) != nil {
		return "", 0, 0, httperror.BadRequest("session id must be a UUID")
	}
	eventID, err := strconv.ParseInt(r.PathValue("eventId"), 10, 64)
	if err != nil || eventID <= 0 {
		return "", 0, 0, httperror.BadRequest("event id must be a positive integer")
	}
	idx, err = strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= maxImageIndex {
		return "", 0, 0, httperror.BadRequest("image index out of range")
	}
	return sessionID, eventID, idx, nil
}
