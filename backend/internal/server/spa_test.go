package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandler_CacheHeaders(t *testing.T) {
	fs := fstest.MapFS{
		"index.html":          {Data: []byte("<html></html>")},
		"sw.js":               {Data: []byte("// service worker")},
		"manifest.webmanifest": {Data: []byte(`{"name":"test"}`)},
		"assets/app.js":       {Data: []byte("// app")},
	}
	h := &spaHandler{fs: fs}

	tests := []struct {
		path         string
		wantCache    string
		wantNoCache  bool
	}{
		{"/sw.js", "no-cache", true},
		{"/manifest.webmanifest", "no-cache", true},
		{"/assets/app.js", "", false},
		{"/index.html", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			cc := w.Header().Get("Cache-Control")
			if tt.wantNoCache && cc != tt.wantCache {
				t.Errorf("expected Cache-Control %q, got %q", tt.wantCache, cc)
			}
			if !tt.wantNoCache && cc != "" {
				t.Errorf("expected no Cache-Control header, got %q", cc)
			}
		})
	}
}

func TestSPAHandler_Fallback(t *testing.T) {
	fs := fstest.MapFS{
		"index.html": {Data: []byte("<html>app</html>")},
	}
	h := &spaHandler{fs: fs}

	req := httptest.NewRequest("GET", "/unknown/route", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	if body := w.Body.String(); body != "<html>app</html>" {
		t.Errorf("expected index.html content, got %q", body)
	}
}
