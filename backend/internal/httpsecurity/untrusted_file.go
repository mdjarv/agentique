package httpsecurity

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// Agent-written bytes served from the application's own origin — session
// files, and every file under a project directory an agent can write to —
// are a stored-XSS primitive if any of them reaches the browser as an active
// document. Agents act on untrusted input (repo contents, web pages, tool
// output), and script on this origin can drive the whole authenticated API,
// including opening a fullAuto session.
//
// So the policy is an allowlist of types that cannot execute, and a download
// for everything else. `.html` and `.svg` are deliberately NOT on the list:
// both run script when navigated to directly, and the chat's markdown renderer
// only intercepts .md/.mdx links, so every other extension reaches the browser
// as an ordinary same-origin navigation.
//
// Downloads still work — an attachment is viewable once saved, it just cannot
// execute in the app's origin on the way there. A client that fetches the
// bytes itself (the file browser's preview) is unaffected by the disposition.
var inertContentTypes = map[string]string{
	// Raster images: what agents actually embed (Playwright screenshots).
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/vnd.microsoft.icon",

	// Text the UI fetches and renders itself, so it never needs to be a document.
	".md":   "text/plain; charset=utf-8",
	".mdx":  "text/plain; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".json": "application/json",

	// Media elements do not execute their payload.
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
}

// UntrustedFileCSP is the policy every agent-written file is served under.
// nosniff makes the declared type binding; this is the backstop if it ever is
// not — a sandboxed document has an opaque origin and no script, so it cannot
// reach the API even if a browser renders it.
const UntrustedFileCSP = "default-src 'none'; sandbox"

// UntrustedFileDisposition returns the Content-Type and Content-Disposition to
// serve an agent-written file with, judged by name alone — never by sniffing
// the bytes, which the author controls. An empty disposition means "render
// inline".
func UntrustedFileDisposition(name string) (contentType, disposition string) {
	ext := strings.ToLower(filepath.Ext(name))
	if ct, inert := inertContentTypes[ext]; inert {
		return ct, ""
	}
	// Not provably inert — hand it over as a download under a type no browser
	// will treat as a document, and pin the filename so the extension cannot
	// be re-read from the URL.
	return "application/octet-stream", `attachment; filename="` + sanitizeFilename(filepath.Base(name)) + `"`
}

// SetUntrustedFileHeaders writes the full header set for serving an
// agent-written file: the allowlisted type (or a download), nosniff, and the
// sandbox CSP. Handlers call this rather than setting the headers piecemeal,
// so no route serving such bytes can forget one. It returns the Content-Type
// it chose.
func SetUntrustedFileHeaders(w http.ResponseWriter, name string) string {
	contentType, disposition := UntrustedFileDisposition(name)
	h := w.Header()
	h.Set("Content-Type", contentType)
	if disposition != "" {
		h.Set("Content-Disposition", disposition)
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", UntrustedFileCSP)
	return contentType
}

// sanitizeFilename keeps a quoted filename parameter unambiguous: strip the
// quoting characters and anything that is not printable ASCII rather than
// escaping them.
func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r < 0x20 || r > 0x7e:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// init keeps the extension table honest: every value must be a media type Go
// itself would parse, so a typo fails the build's own tests rather than a
// browser's parser.
func init() {
	for ext, ct := range inertContentTypes {
		if _, _, err := mime.ParseMediaType(ct); err != nil {
			panic("httpsecurity: inertContentTypes[" + ext + "] is not a valid media type: " + ct)
		}
	}
}
