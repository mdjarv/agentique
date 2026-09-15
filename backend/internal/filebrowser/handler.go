package filebrowser

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/content"
	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
	"github.com/mdjarv/agentique/backend/internal/store"
)

const (
	maxTextBytes  = 1 << 20  // 1 MB
	maxImageBytes = 10 << 20 // 10 MB
)

// Handler serves project-scoped file browsing.
type Handler struct {
	Queries *store.Queries
}

type fileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type listResponse struct {
	Path    string      `json:"path"`
	Entries []fileEntry `json:"entries"`
}

// HandleList returns directory contents within a project's root.
// GET /api/projects/{id}/files?path=relative/path
func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	project, err := h.Queries.GetProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}

	relPath := r.URL.Query().Get("path")
	// The listing is confined the way content is (content.OpenRoot): an
	// agent writes this tree, so a symlink is followed only while it stays
	// inside the project, and one that leaves it is left out of the listing.
	root, name, err := content.OpenRoot(project.Path, relPath)
	if errors.Is(err, content.ErrInvalidPath) {
		respondError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err != nil {
		respondError(w, http.StatusNotFound, "path not found")
		return
	}
	defer root.Close()

	dir, err := root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			respondError(w, http.StatusNotFound, "path not found")
		} else {
			respondError(w, http.StatusBadRequest, "invalid path")
		}
		return
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		respondError(w, http.StatusNotFound, "path not found")
		return
	}
	if !info.IsDir() {
		respondError(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	dirEntries, err := dir.ReadDir(-1)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "cannot read directory")
		return
	}

	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entryName := de.Name()
		if entryName == ".git" {
			continue
		}

		fi, err := de.Info()
		if err != nil {
			continue
		}

		// Resolve symlinks to get the real type, inside the root only.
		if de.Type()&os.ModeSymlink != 0 {
			resolved, err := root.Stat(filepath.Join(name, entryName))
			if err != nil {
				continue // broken, or pointing out of the project
			}
			fi = resolved
		}

		entries = append(entries, fileEntry{
			Name:    entryName,
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // dirs first
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	// Return the relative path for the frontend.
	displayPath := relPath
	if displayPath == "" {
		displayPath = "."
	}

	respondJSON(w, http.StatusOK, listResponse{
		Path:    displayPath,
		Entries: entries,
	})
}

// HandleContent serves a file's raw content within a project's root. Only
// provably inert types render inline; see content.Serve.
// GET /api/projects/{id}/files/content?path=relative/path
func (h *Handler) HandleContent(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	project, err := h.Queries.GetProject(r.Context(), projectID)
	if err != nil {
		respondError(w, http.StatusNotFound, "project not found")
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		respondError(w, http.StatusBadRequest, "path is required")
		return
	}

	contentType, _ := httpsecurity.UntrustedFileDisposition(relPath)
	limit := int64(maxTextBytes)
	if isImageContentType(contentType) {
		limit = maxImageBytes
	}

	// A project directory is written by agents, and this route answers on the
	// app's own origin, so the bytes go through the one untrusted-file path:
	// the type comes from the allowlist and never from the sniffer. The file
	// browser fetches the bytes itself, so a download disposition costs it
	// nothing.
	item, err := content.OpenInRoot(project.Path, relPath, limit)
	switch {
	case errors.Is(err, content.ErrNotFound):
		respondError(w, http.StatusNotFound, "file not found")
		return
	case err != nil:
		content.RespondError(w, err)
		return
	}
	content.Serve(w, r, item)
}

func isImageContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/")
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
