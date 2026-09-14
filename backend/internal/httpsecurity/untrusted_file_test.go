package httpsecurity

import (
	"net/http/httptest"
	"testing"
)

func TestUntrustedFileDisposition(t *testing.T) {
	cases := []struct {
		name        string
		wantType    string
		wantAttach  bool
		explanation string
	}{
		{"shot.png", "image/png", false, "screenshots must still render inline"},
		{"a.JPEG", "image/jpeg", false, "extension match is case-insensitive"},
		{"notes.md", "text/plain; charset=utf-8", false, "the UI fetches and renders markdown itself"},
		{"data.json", "application/json", false, "inert"},
		{"clip.mp4", "video/mp4", false, "media elements do not execute their payload"},

		{"report.html", "application/octet-stream", true, "HTML would run as a same-origin document"},
		{"page.htm", "application/octet-stream", true, "same as .html"},
		{"pic.svg", "application/octet-stream", true, "SVG runs script when navigated to directly"},
		{"x.xhtml", "application/octet-stream", true, "same as .html"},
		{"app.js", "application/octet-stream", true, "never serve script from this origin"},
		{"doc.pdf", "application/octet-stream", true, "PDF viewers are an active surface"},
		{"README", "application/octet-stream", true, "no extension means no proof it is inert"},
		{"weird.unknown", "application/octet-stream", true, "unknown types are sniffable"},
	}
	for _, c := range cases {
		ct, disp := UntrustedFileDisposition(c.name)
		if ct != c.wantType {
			t.Errorf("%s: content type = %q, want %q (%s)", c.name, ct, c.wantType, c.explanation)
		}
		if attached := disp != ""; attached != c.wantAttach {
			t.Errorf("%s: attachment = %v, want %v (%s)", c.name, attached, c.wantAttach, c.explanation)
		}
	}
}

func TestSanitizeFilenameKeepsTheHeaderUnambiguous(t *testing.T) {
	for in, want := range map[string]string{
		`plain.txt`:      `plain.txt`,
		`a"b.txt`:        `a_b.txt`,
		"a\r\nb.txt":     `a__b.txt`,
		`back\slash.txt`: `back_slash.txt`,
		``:               `download`,
	} {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetUntrustedFileHeaders(t *testing.T) {
	for _, name := range []string{"evil.html", "shot.png"} {
		rec := httptest.NewRecorder()
		SetUntrustedFileHeaders(rec, name)
		for header, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "default-src 'none'; sandbox",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", name, header, got, want)
			}
		}
	}
}
