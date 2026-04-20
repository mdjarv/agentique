package server

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// spaHandler serves a single-page application from an embedded filesystem.
// If the requested file exists, it is served directly. Otherwise, index.html
// is served as a fallback so the SPA client-side router can handle the path.
type spaHandler struct {
	fs fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the URL path.
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}

	// Try to open the requested file.
	f, err := h.fs.Open(p)
	if err != nil {
		// Missing asset-style requests must 404, not fall back to index.html.
		// Serving HTML for a .js/.css request produces a MIME mismatch in the
		// browser and can mask stale-PWA precache bugs by trapping the client
		// in a loop where no new module ever loads.
		if isAssetRequest(p) {
			httperror.RespondError(w, httperror.NotFound("asset not found").WithCause(err))
			return
		}
		h.serveIndex(w)
		return
	}
	defer f.Close()

	// If the path is a directory, serve index.html instead.
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		h.serveIndex(w)
		return
	}

	// Set Content-Type based on file extension.
	ext := filepath.Ext(p)
	ct := mime.TypeByExtension(ext)
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	// Service worker and manifest must not be aggressively cached —
	// stale sw.js prevents PWA auto-updates from being detected.
	if p == "sw.js" || ext == ".webmanifest" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

// isAssetRequest reports whether the path looks like a static asset (has a
// non-.html extension or sits under assets/). Navigation requests to SPA
// routes have no extension and must still fall back to index.html.
func isAssetRequest(p string) bool {
	if strings.HasPrefix(p, "assets/") {
		return true
	}
	ext := filepath.Ext(p)
	return ext != "" && ext != ".html"
}

func (h *spaHandler) serveIndex(w http.ResponseWriter) {
	f, err := h.fs.Open("index.html")
	if err != nil {
		httperror.RespondError(w, httperror.Internal("index.html not found", err))
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}
