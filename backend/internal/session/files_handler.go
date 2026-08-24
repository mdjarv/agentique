package session

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/paths"
)

// FilesHandler serves persistent session files (screenshots, exports, etc.).
type FilesHandler struct{}

// HandleServe serves a file from a session's persistent files directory.
// Route: GET /api/sessions/{id}/files/{filepath...}
func (h *FilesHandler) HandleServe(w http.ResponseWriter, r *http.Request) {
	// A {id} wildcard is NOT one path segment: Go's ServeMux matches on the
	// escaped path and unescapes the capture, so %2F arrives here as a real
	// separator. Validate before joining — the "is the result still inside the
	// session dir" check below derives its root from this same value, so it
	// cannot catch an escape on its own.
	sessionID := r.PathValue("id")
	if uuid.Validate(sessionID) != nil {
		httperror.RespondError(w, httperror.BadRequest("session id must be a UUID"))
		return
	}

	// Extract the filepath after /api/sessions/{id}/files/
	filePath := r.PathValue("filepath")
	if filePath == "" {
		httperror.RespondError(w, httperror.BadRequest("file path is required"))
		return
	}

	// Sanitize: reject path traversal attempts while allowing ordinary
	// filenames such as "notes..md".
	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) ||
		cleaned == "." ||
		cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		httperror.RespondError(w, httperror.BadRequest("invalid file path"))
		return
	}

	sessionDir := filepath.Join(paths.SessionFilesDir(), sessionID)
	fullPath := filepath.Join(sessionDir, cleaned)

	// Double-check the resolved path is still within the session directory.
	if !strings.HasPrefix(fullPath, sessionDir+string(os.PathSeparator)) {
		httperror.RespondError(w, httperror.BadRequest("invalid file path"))
		return
	}

	// Resolve symlinks and re-check to prevent symlink-based path traversal.
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			httperror.RespondError(w, httperror.NotFound("file not found"))
			return
		}
		httperror.RespondError(w, httperror.BadRequest("invalid file path"))
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(sessionDir)
	if err != nil {
		httperror.RespondError(w, httperror.Internal("resolve session dir", err))
		return
	}
	if resolvedPath != resolvedRoot && !strings.HasPrefix(resolvedPath, resolvedRoot+string(os.PathSeparator)) {
		httperror.RespondError(w, httperror.BadRequest("invalid file path"))
		return
	}

	// Never serve a directory listing of an agent's scratch space.
	if info, statErr := os.Stat(resolvedPath); statErr != nil || info.IsDir() {
		httperror.RespondError(w, httperror.NotFound("file not found"))
		return
	}

	// This content is agent-written and this origin is the application's own,
	// so decide the type here instead of letting the extension (or the
	// sniffer) decide it. See files_content_type.go.
	contentType, disposition := sessionFileDisposition(resolvedPath)
	w.Header().Set("Content-Type", contentType)
	if disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	// nosniff makes the declared type binding; the sandbox CSP is the backstop
	// if it ever is not — a sandboxed document has an opaque origin and no
	// script, so it cannot reach the API even if a browser renders it.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")

	// ServeFile keeps a Content-Type we already set, and adds range/caching.
	http.ServeFile(w, r, resolvedPath)
}
