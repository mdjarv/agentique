package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// Browser-facing hardening. These are response headers, so they belong in one
// middleware rather than at each handler: a route added later inherits them
// instead of having to remember them.
//
// The CSP is the part that carries weight. Script on this origin can drive the
// entire authenticated API — including opening a fullAuto session — so an
// injection anywhere in the UI is equivalent to code execution on the host.
// frame-ancestors matters for the same reason from the other direction: the
// approve-tool, merge, and apply-upgrade controls are all one click.
func securityHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// The declared Content-Type is binding. Without this, a sniffer can
		// promote agent-written bytes to an active document.
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// X-Frame-Options for the browsers and proxies that still only read it;
		// frame-ancestors in the CSP below is the modern spelling.
		h.Set("X-Frame-Options", "DENY")

		// The API and the file route set their own, tighter policies (the file
		// route sandboxes; both are stricter than the document policy). Only
		// documents need the SPA policy.
		if csp != "" && !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/mcp" {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

// inlineScriptRe finds inline <script> bodies. Anything carrying a src is an
// external script and is covered by 'self' instead.
var inlineScriptRe = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// spaCSP builds the document policy for the embedded SPA.
//
// index.html ships one inline bootstrap script (it applies the stored theme
// before first paint, which a deferred module script cannot do). Rather than
// weaken script-src with 'unsafe-inline' — which would defeat the directive
// entirely — its sha256 is computed from the bundle that is actually embedded.
// That way a frontend rebuild can change the script freely and the policy
// follows it, with no hash to keep in sync by hand.
func spaCSP(fsys fs.FS) string {
	directives := []string{
		"default-src 'self'",
		"script-src 'self'" + inlineScriptHashes(fsys),
		// Mermaid and the graph view inject <style> elements at runtime, so
		// inline styles cannot be forbidden here. Style injection is a
		// defacement risk, not an API-access one.
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"font-src 'self' data: https://fonts.gstatic.com",
		"img-src 'self' data: blob:",
		"media-src 'self' blob:",
		// Paired machines are operator-configured HTTPS origins, and the client
		// fans out REST and WebSockets to every one of them (docs/multi-machine.md).
		// This cannot be narrowed to 'self' without breaking multi-machine.
		"connect-src 'self' https: wss: ws:",
		"worker-src 'self' blob:",
		"manifest-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}
	return strings.Join(directives, "; ")
}

// inlineScriptHashes returns the sha256 source expressions for every inline
// script in index.html, ready to append to script-src.
func inlineScriptHashes(fsys fs.FS) string {
	if fsys == nil {
		return ""
	}
	f, err := fsys.Open("index.html")
	if err != nil {
		// No embedded bundle (backend-only build or a test server). The stub
		// index has no inline script, so there is nothing to allow.
		return ""
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		slog.Warn("csp: could not read index.html for inline script hashes", "error", err)
		return ""
	}

	var out strings.Builder
	seen := make(map[string]bool)
	for _, m := range inlineScriptRe.FindAllSubmatch(raw, -1) {
		if strings.Contains(strings.ToLower(string(m[1])), "src=") {
			continue
		}
		body := m[2]
		if len(strings.TrimSpace(string(body))) == 0 {
			continue
		}
		sum := sha256.Sum256(body)
		expr := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if seen[expr] {
			continue
		}
		seen[expr] = true
		out.WriteString(" ")
		out.WriteString(expr)
	}
	return out.String()
}
