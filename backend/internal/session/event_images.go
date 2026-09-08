package session

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// A history snapshot never carries image bytes.
//
// Tool results embed screenshots as base64 data URLs, ~640KB each, and a
// browser-driving session accumulates hundreds of them: one such session was
// 46MB of history, of which 43MB was JPEG. Every client loaded all of it on
// boot, over one serial socket, before the session it had actually opened.
//
// So the history builder replaces each inline image with a reference to
// this endpoint, which extracts the bytes from the persisted event row on
// demand. The row is the source of truth and is left untouched — the rewrite
// happens at read time, which is what makes it cover every session ever
// recorded rather than only those persisted after the change. Live events
// still stream the data URL inline: one image at a time is the size the
// transcript already handles, and the next history load turns it into a ref.

// inlineImageTypes is the set of media types this endpoint will serve
// inline. It mirrors the raster half of the session-files allowlist
// (files_content_type.go) for the same reason: these bytes are agent-written
// and served from the app's own origin, so only types that cannot execute
// are rendered; anything else is a download.
var inlineImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/avif": true,
	"image/bmp":  true,
}

// maxImageIndex bounds the block index a URL may name. A tool result with
// more image blocks than this does not exist in practice, and the bound
// keeps a hostile path from driving an unbounded loop.
const maxImageIndex = 1024

// imageRefPath is the origin-relative URL the history builder substitutes
// for the idx-th inline image of an event.
func imageRefPath(sessionID string, eventID int64, idx int) string {
	return fmt.Sprintf("/api/sessions/%s/events/%d/images/%d", sessionID, eventID, idx)
}

var dataURLMarker = []byte(`"data:`)

// detachToolResultImages rewrites every image block of a tool_result whose
// url is an inline data URL to a reference. Events with no inline image are
// returned as-is, bytes untouched.
func detachToolResultImages(sessionID string, eventID int64, data json.RawMessage) json.RawMessage {
	if !bytes.Contains(data, dataURLMarker) {
		return data
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	blocks, ok := m["content"].([]any)
	if !ok {
		return data
	}
	changed := false
	for i, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		url, _ := block["url"].(string)
		if !strings.HasPrefix(url, "data:") {
			continue
		}
		if _, has := block["mediaType"]; !has {
			if mt, _, err := splitDataURL(url); err == nil {
				block["mediaType"] = mt
			}
		}
		block["url"] = imageRefPath(sessionID, eventID, i)
		changed = true
	}
	if !changed {
		return data
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return json.RawMessage(out)
}

// detachAttachmentImages does the same for a prompt's attachments, in place.
func detachAttachmentImages(sessionID string, eventID int64, attachments []QueryAttachment) {
	for i := range attachments {
		if strings.HasPrefix(attachments[i].DataUrl, "data:") {
			attachments[i].DataUrl = imageRefPath(sessionID, eventID, i)
		}
	}
}

// inlineImageAt returns the idx-th inline data URL carried by a persisted
// event: a tool_result's image block, or a prompt's attachment.
func inlineImageAt(eventType string, data string, idx int) (string, bool) {
	switch eventType {
	case "tool_result":
		var tr struct {
			Content []struct {
				URL string `json:"url"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(data), &tr) != nil || idx >= len(tr.Content) {
			return "", false
		}
		return tr.Content[idx].URL, strings.HasPrefix(tr.Content[idx].URL, "data:")
	case "prompt":
		var p struct {
			Attachments []QueryAttachment `json:"attachments"`
		}
		if json.Unmarshal([]byte(data), &p) != nil || idx >= len(p.Attachments) {
			return "", false
		}
		return p.Attachments[idx].DataUrl, strings.HasPrefix(p.Attachments[idx].DataUrl, "data:")
	}
	return "", false
}

// splitDataURL parses "data:<mediatype>;base64,<payload>" into its lowercase
// media type (parameters dropped) and the still-encoded payload. Only the
// base64 form is accepted; nothing here writes a percent-encoded image.
func splitDataURL(u string) (mediaType, payload string, err error) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return "", "", fmt.Errorf("not a data URL")
	}
	header, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return "", "", fmt.Errorf("data URL has no payload")
	}
	parts := strings.Split(header, ";")
	if parts[len(parts)-1] != "base64" {
		return "", "", fmt.Errorf("data URL is not base64")
	}
	mediaType = strings.ToLower(strings.TrimSpace(parts[0]))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	return mediaType, payload, nil
}

func decodeDataURL(u string) (mediaType string, body []byte, err error) {
	mediaType, payload, err := splitDataURL(u)
	if err != nil {
		return "", nil, err
	}
	body, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Some producers omit the padding.
		body, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return "", nil, fmt.Errorf("decode data URL: %w", err)
	}
	return mediaType, body, nil
}

// EventImageHandler serves the inline images of persisted events.
type EventImageHandler struct {
	Queries *store.Queries
}

// HandleServe answers GET /api/sessions/{id}/events/{eventId}/images/{idx}.
func (h *EventImageHandler) HandleServe(w http.ResponseWriter, r *http.Request) {
	// Every path parameter is validated for what it is before it reaches a
	// query: a {param} is not one path segment (see FilesHandler).
	sessionID := r.PathValue("id")
	if uuid.Validate(sessionID) != nil {
		httperror.RespondError(w, httperror.BadRequest("session id must be a UUID"))
		return
	}
	eventID, err := strconv.ParseInt(r.PathValue("eventId"), 10, 64)
	if err != nil || eventID <= 0 {
		httperror.RespondError(w, httperror.BadRequest("event id must be a positive integer"))
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil || idx < 0 || idx >= maxImageIndex {
		httperror.RespondError(w, httperror.BadRequest("image index out of range"))
		return
	}

	// The session id is part of the key, so an event of another session is
	// not found rather than served.
	row, err := h.Queries.GetSessionEvent(r.Context(), store.GetSessionEventParams{ID: eventID, SessionID: sessionID})
	if err != nil {
		httperror.RespondError(w, httperror.NotFound("event not found"))
		return
	}
	dataURL, ok := inlineImageAt(row.Type, row.Data, idx)
	if !ok {
		httperror.RespondError(w, httperror.NotFound("image not found"))
		return
	}
	mediaType, body, err := decodeDataURL(dataURL)
	if err != nil {
		httperror.RespondError(w, httperror.NotFound("image not found"))
		return
	}

	if inlineImageTypes[mediaType] {
		w.Header().Set("Content-Type", mediaType)
	} else {
		// Not provably inert: hand it over as a download, never as a document
		// on this origin.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="event-`+strconv.FormatInt(eventID, 10)+`-`+strconv.Itoa(idx)+`"`)
	}
	// An event row is never rewritten, so the bytes behind a URL never change.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
