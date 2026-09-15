// Package content serves agent-written bytes from the application's own origin.
//
// Three routes hand such bytes to a browser — a session's files, the images the
// history builder detached from persisted events, and a project's files — and a
// fourth relays the first two from the paired machine that owns the session.
// Each used to parse its route, find its bytes, choose its headers and write
// them in one function, with its own copy of the traversal guard and, for event
// images, its own copy of the type allowlist. So the job is split here:
//
//   - a source finds the bytes and returns an [Item] (this package's
//     [OpenInRoot] for files on disk, [FromBytes] for bytes already in hand, a
//     relay for another machine's);
//   - [Serve] writes any Item with the one header policy, judged from
//     Item.Name by [httpsecurity.SetUntrustedFileHeaders] — never from headers
//     another machine sent, which is what keeps a relayed file exactly as inert
//     as a local one;
//   - [RespondError] turns a source's sentinel into a fixed response.
package content

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
)

// The failures a source reports. Every one maps to a fixed message in
// [RespondError]; the detail, when there is any, is wrapped for the log.
var (
	// ErrNotFound: nothing by that name, or a directory.
	ErrNotFound = errors.New("not found")
	// ErrInvalidPath: the name could not refer to anything inside the root.
	ErrInvalidPath = errors.New("invalid path")
	// ErrTooLarge: the item exists and is over the caller's bound.
	ErrTooLarge = errors.New("too large")
	// ErrUnavailable: the machine that owns the item did not answer.
	ErrUnavailable = errors.New("the machine that owns this did not answer")
	// ErrUnsupported: the machine that owns the item runs a release that
	// cannot hand it over.
	ErrUnsupported = errors.New("the machine that owns this cannot relay it")
)

// Item is one file ready to serve. The caller of a source owns it and must
// Close it; [Serve] does.
type Item struct {
	// Name decides the Content-Type and disposition, through the allowlist.
	// Only its extension and base name are read.
	Name string
	// Size is the body's length, or -1 when the source cannot say.
	Size    int64
	ModTime time.Time
	// Immutable marks bytes that never change behind their URL.
	Immutable bool
	// Body is the content. An io.ReadSeeker gets range and conditional
	// requests from [http.ServeContent]; anything else is copied.
	Body  io.Reader
	close func() error
}

// Close releases whatever the source holds open behind the body.
func (i Item) Close() error {
	if i.close == nil {
		return nil
	}
	return i.close()
}

// NewItem builds an Item whose Close runs closer. It is for sources outside
// this package, such as a relay holding a response body open.
func NewItem(name string, size int64, modTime time.Time, body io.Reader, closer func() error) Item {
	return Item{Name: name, Size: size, ModTime: modTime, Body: body, close: closer}
}

// Serve writes item with the untrusted-file header set and closes it.
func Serve(w http.ResponseWriter, r *http.Request, item Item) {
	defer item.Close()
	httpsecurity.SetUntrustedFileHeaders(w, item.Name)
	if item.Immutable {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
	// ServeContent keeps the Content-Type already set and adds range and
	// conditional handling.
	if rs, ok := item.Body.(io.ReadSeeker); ok {
		http.ServeContent(w, r, item.Name, item.ModTime, rs)
		return
	}
	if item.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(item.Size, 10))
	}
	if !item.ModTime.IsZero() {
		w.Header().Set("Last-Modified", item.ModTime.UTC().Format(http.TimeFormat))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	// A copy that fails mid-body has already sent its status; the truncated
	// response is what the client sees, and there is no one else to tell.
	_, _ = io.Copy(w, item.Body)
}

// RespondError writes the fixed response for a source's failure. Anything that
// is not one of this package's sentinels is an internal error.
func RespondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httperror.RespondError(w, httperror.NotFound("file not found"))
	case errors.Is(err, ErrInvalidPath):
		httperror.RespondError(w, httperror.BadRequest("invalid file path"))
	case errors.Is(err, ErrTooLarge):
		httperror.RespondError(w, &httperror.Error{Status: http.StatusRequestEntityTooLarge, Message: "file too large"})
	// A machine that is asleep, or a release not upgraded yet, is an ordinary
	// state for a paired machine, not a fault in this server.
	case errors.Is(err, ErrUnavailable):
		httperror.RespondError(w, httperror.BadGateway("the machine that owns this file did not answer", err).
			WithLogLevel(slog.LevelWarn))
	case errors.Is(err, ErrUnsupported):
		httperror.RespondError(w, (&httperror.Error{Status: http.StatusNotImplemented,
			Message: "the machine that owns this file runs a release that cannot relay it", Cause: err}).
			WithLogLevel(slog.LevelWarn))
	default:
		httperror.RespondError(w, httperror.Internal("serve file", err))
	}
}
