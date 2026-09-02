package filesystem

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Handler handles filesystem browsing HTTP requests.
type Handler struct{}

type browseResponse struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	// Segments is the path split into breadcrumbs by THIS server, root (or
	// volume, "C:\" on Windows) first. The server computes them because only
	// it knows its own separator and root shape — a client splitting on "/"
	// renders a Windows path as one giant crumb and a dead root button.
	Segments []segment `json:"segments"`
	Entries  []entry   `json:"entries"`
}

type segment struct {
	// Name is what the crumb shows ("Users", or "C:\" / "/" for the root).
	Name string `json:"name"`
	// Path is the absolute path navigating to this crumb requests.
	Path string `json:"path"`
}

// pathSegments walks dirPath up to its root, emitting one breadcrumb per
// component. The loop ends where Dir stops changing the path — "/" on unix,
// the volume ("C:\", `\\host\share`) on Windows — and that terminal element
// is itself the first crumb, so the client needs no hardcoded root button.
func pathSegments(dirPath string) []segment {
	var segs []segment
	for {
		parent := filepath.Dir(dirPath)
		if parent == dirPath {
			return append([]segment{{Name: dirPath, Path: dirPath}}, segs...)
		}
		segs = append([]segment{{Name: filepath.Base(dirPath), Path: dirPath}}, segs...)
		dirPath = parent
	}
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
			httperror.RespondError(w, httperror.Internal("determine home directory", err))
			return
		}
		dirPath = home
	}

	dirPath = filepath.Clean(dirPath)
	if !filepath.IsAbs(dirPath) {
		httperror.RespondError(w, httperror.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		httperror.RespondError(w, httperror.NotFound("path does not exist"))
		return
	}
	if os.IsPermission(err) {
		httperror.RespondError(w, httperror.BadRequest("permission denied"))
		return
	}
	if err != nil {
		httperror.RespondError(w, httperror.Internal("stat path", err))
		return
	}
	if !info.IsDir() {
		httperror.RespondError(w, httperror.BadRequest("path is not a directory"))
		return
	}

	dirEntries, err := os.ReadDir(dirPath)
	if os.IsPermission(err) {
		httperror.RespondError(w, httperror.BadRequest("permission denied"))
		return
	}
	if err != nil {
		httperror.RespondError(w, httperror.Internal("read directory", err))
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

	httperror.JSON(w, http.StatusOK, browseResponse{
		Path:     dirPath,
		Parent:   parent,
		Segments: pathSegments(dirPath),
		Entries:  entries,
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
		httperror.RespondError(w, httperror.BadRequest("path is required"))
		return
	}

	dirPath = filepath.Clean(dirPath)
	if !filepath.IsAbs(dirPath) {
		httperror.RespondError(w, httperror.BadRequest("path must be absolute"))
		return
	}

	info, err := os.Stat(dirPath)
	if err == nil {
		httperror.JSON(w, http.StatusOK, validateResponse{
			Exists:       true,
			IsDirectory:  info.IsDir(),
			ParentExists: true,
		})
		return
	}

	parentInfo, parentErr := os.Stat(filepath.Dir(dirPath))
	httperror.JSON(w, http.StatusOK, validateResponse{
		Exists:       false,
		IsDirectory:  false,
		ParentExists: parentErr == nil && parentInfo.IsDir(),
	})
}
