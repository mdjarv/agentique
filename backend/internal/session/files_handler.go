package session

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/allbin/agentique/backend/internal/httperror"
	"github.com/allbin/agentique/backend/internal/paths"
)

// FilesHandler serves persistent session files (screenshots, exports, etc.).
type FilesHandler struct{}

// HandleServe serves a file from a session's persistent files directory.
// Route: GET /api/sessions/{id}/files/{filepath...}
func (h *FilesHandler) HandleServe(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		httperror.RespondError(w, httperror.BadRequest("session id is required"))
		return
	}

	// Extract the filepath after /api/sessions/{id}/files/
	filePath := r.PathValue("filepath")
	if filePath == "" {
		httperror.RespondError(w, httperror.BadRequest("file path is required"))
		return
	}

	// Sanitize: reject path traversal attempts.
	cleaned := filepath.Clean(filePath)
	if strings.Contains(cleaned, "..") || filepath.IsAbs(cleaned) {
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

	http.ServeFile(w, r, resolvedPath)
}
