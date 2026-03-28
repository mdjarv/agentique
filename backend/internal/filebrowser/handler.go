package filebrowser

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/allbin/agentique/backend/internal/store"
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
	absPath, err := safePath(project.Path, relPath)
	if errors.Is(err, errPathEscape) {
		respondError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err != nil {
		respondError(w, http.StatusNotFound, "path not found")
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		respondError(w, http.StatusNotFound, "path not found")
		return
	}
	if !info.IsDir() {
		respondError(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "cannot read directory")
		return
	}

	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if name == ".git" {
			continue
		}

		fi, err := de.Info()
		if err != nil {
			continue
		}

		// Resolve symlinks to get the real type.
		fullPath := filepath.Join(absPath, name)
		if de.Type()&os.ModeSymlink != 0 {
			resolved, err := os.Stat(fullPath)
			if err != nil {
				continue // broken symlink
			}
			fi = resolved
		}

		entries = append(entries, fileEntry{
			Name:    name,
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

// HandleContent serves a file's raw content within a project's root.
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

	absPath, err := safePath(project.Path, relPath)
	if errors.Is(err, errPathEscape) {
		respondError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err != nil {
		respondError(w, http.StatusNotFound, "file not found")
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		respondError(w, http.StatusNotFound, "file not found")
		return
	}
	if fi.IsDir() {
		respondError(w, http.StatusBadRequest, "path is a directory")
		return
	}

	ct := contentType(absPath)
	limit := maxTextBytes
	if isImageContentType(ct) {
		limit = maxImageBytes
	}

	if fi.Size() > int64(limit) {
		respondError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	f, err := os.Open(absPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "cannot open file")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", ct)
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func contentType(path string) string {
	ext := filepath.Ext(path)
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	// Fallback: read first 512 bytes to sniff.
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	return http.DetectContentType(buf[:n])
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
