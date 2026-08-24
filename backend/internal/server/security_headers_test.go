package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSecurityHeadersOnDocuments(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := securityHeaders("default-src 'self'", next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'self'",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// API responses carry the hardening headers but not the document policy — the
// session-file route sets its own, stricter sandbox policy and must not have it
// overwritten by the SPA one.
func TestSecurityHeadersLeaveAPICSPToTheHandler(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeaders("default-src 'self'", next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/x/files/y.png", nil))

	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
		t.Errorf("CSP = %q, want the handler's own sandbox policy", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff = %q, want nosniff", got)
	}
}

func TestSPACSPHashesTheInlineBootstrapScript(t *testing.T) {
	inline := "\n  var t = 1;\n"
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<html><head><script>` + inline + `</script>` +
				`<script type="module" src="/assets/main.js"></script></head></html>`)},
	}

	csp := spaCSP(fsys)
	sum := sha256.Sum256([]byte(inline))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"

	if !strings.Contains(csp, want) {
		t.Errorf("CSP %q does not allow the inline script by hash (%s)", csp, want)
	}
	// The whole point of hashing is not needing this.
	if strings.Contains(csp, "'unsafe-inline'") && strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Error("script-src must not fall back to 'unsafe-inline'")
	}
	for _, directive := range []string{"frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP is missing %q: %s", directive, csp)
		}
	}
}

func TestSPACSPWithoutAnEmbeddedBundle(t *testing.T) {
	// Backend-only builds embed no index.html; the policy must still be valid
	// and must not silently gain 'unsafe-inline'.
	csp := spaCSP(fstest.MapFS{})
	if !strings.Contains(csp, "script-src 'self';") {
		t.Errorf("CSP = %q, want a bare script-src 'self'", csp)
	}
}
