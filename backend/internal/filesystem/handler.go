package filesystem

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/allbin/agentique/backend/internal/httperr"
	"github.com/allbin/agentique/backend/internal/respond"
)

// Handler handles filesystem browsing HTTP requests.
type Handler struct{}

type browseResponse struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`
	Entries []entry `json:"entries"`
}

type entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsGitRepo bool   `json:"isGitRepo"`
}

// HandleBrowse lists subdirectories at the requested path.
// Query param "path" defaults to the user's home directory.
func (h *Handler) HandleBrowse(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			respond.Error(w, httperr.Internal("determine home directory", err))
			return
		}
		dirPath = home
	}

	dirPath = filepath.Clean(dirPath)
	if !filepath.IsAbs(dirPath) {
		respond.Error(w, httperr.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		respond.Error(w, httperr.NotFound("path does not exist"))
		return
	}
	if os.IsPermission(err) {
		respond.Error(w, httperr.BadRequest("permission denied"))
		return
	}
	if err != nil {
		respond.Error(w, httperr.Internal("stat path", err))
		return
	}
	if !info.IsDir() {
		respond.Error(w, httperr.BadRequest("path is not a directory"))
		return
	}

	dirEntries, err := os.ReadDir(dirPath)
	if os.IsPermission(err) {
		respond.Error(w, httperr.BadRequest("permission denied"))
		return
	}
	if err != nil {
		respond.Error(w, httperr.Internal("read directory", err))
		return
	}

	entries := make([]entry, 0)
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dirPath, name)

		// Resolve symlinks — skip if broken or not a directory.
		fi, err := os.Stat(fullPath)
		if err != nil || !fi.IsDir() {
			continue
		}

		isGit := false
		if gitInfo, err := os.Stat(filepath.Join(fullPath, ".git")); err == nil && gitInfo != nil {
			isGit = true
		}

		entries = append(entries, entry{
			Name:      name,
			Path:      fullPath,
			IsGitRepo: isGit,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(dirPath)
	if parent == dirPath {
		parent = ""
	}

	respond.JSON(w, http.StatusOK, browseResponse{
		Path:    dirPath,
		Parent:  parent,
		Entries: entries,
	})
}

type validateResponse struct {
	Exists       bool `json:"exists"`
	IsDirectory  bool `json:"isDirectory"`
	ParentExists bool `json:"parentExists"`
}

// HandleValidate checks whether a path exists and whether its parent exists.
func (h *Handler) HandleValidate(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		respond.Error(w, httperr.BadRequest("path is required"))
		return
	}

	dirPath = filepath.Clean(dirPath)
	if !filepath.IsAbs(dirPath) {
		respond.Error(w, httperr.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(dirPath)
	if err == nil {
		respond.JSON(w, http.StatusOK, validateResponse{
			Exists:       true,
			IsDirectory:  info.IsDir(),
			ParentExists: true,
		})
		return
	}

	parentInfo, parentErr := os.Stat(filepath.Dir(dirPath))
	respond.JSON(w, http.StatusOK, validateResponse{
		Exists:       false,
		IsDirectory:  false,
		ParentExists: parentErr == nil && parentInfo.IsDir(),
	})
}
