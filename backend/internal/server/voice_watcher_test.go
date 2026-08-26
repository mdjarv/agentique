package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadProjectGuide(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("  # Guide\n\nBe careful.  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readProjectGuide(dir)
	if !strings.Contains(got, "Be careful.") {
		t.Errorf("guide = %q, want the file's content", got)
	}
	if got != strings.TrimSpace(got) {
		t.Error("guide should be trimmed")
	}
}

// A project without a CLAUDE.md is ordinary. A drafter that refused to work
// over a missing file would be worse than a vague one.
func TestReadProjectGuideToleratesAbsence(t *testing.T) {
	if got := readProjectGuide(t.TempDir()); got != "" {
		t.Errorf("guide = %q, want empty for a project with no CLAUDE.md", got)
	}
	if got := readProjectGuide(""); got != "" {
		t.Errorf("guide = %q, want empty for an unset path", got)
	}
	if got := readProjectGuide("/nonexistent/nowhere"); got != "" {
		t.Errorf("guide = %q, want empty for a missing directory", got)
	}
}

// Everything in the context goes to the speech vendor on every call, so the
// budget is a real limit rather than a truncation accident.
func TestReadProjectGuideIsBounded(t *testing.T) {
	dir := t.TempDir()
	// Multi-byte on purpose: the cut must land on a rune boundary.
	big := strings.Repeat("é", maxProjectContext*2)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readProjectGuide(dir)
	if !strings.Contains(got, "truncated") {
		t.Error("a clipped guide should say so, or it reads as the whole file")
	}
	if !strings.Contains(got, "é") {
		t.Error("truncation mangled the text")
	}
	if n := len([]rune(got)); n > maxProjectContext+40 {
		t.Errorf("guide kept %d runes, want roughly the %d budget", n, maxProjectContext)
	}
}

func TestClampSpoken(t *testing.T) {
	if got := clampSpoken("  two   spaces\nand a newline  "); got != "two spaces and a newline" {
		t.Errorf("clampSpoken() = %q, want collapsed whitespace", got)
	}
	long := strings.Repeat("ü", maxSpokenSummary*2)
	got := clampSpoken(long)
	if n := len([]rune(got)); n > maxSpokenSummary+1 {
		t.Errorf("kept %d runes, want <= %d plus an ellipsis", n, maxSpokenSummary+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("a clipped summary should show that it was cut")
	}
}

// The only auto-approve mode that never stops for a prompt is fullAuto. Under
// accept-edits a Bash prompt still blocks, and with no spoken approval the run
// would stall with nobody told.
func TestAutoApproveAllIsTheBypassingMode(t *testing.T) {
	if autoApproveAll != "fullAuto" {
		t.Errorf("autoApproveAll = %q; it must match the string runtimeAutoApproveMode maps to runtime.AutoApproveAll", autoApproveAll)
	}
}

func TestClampSummaryStripsMarkdownAndBounds(t *testing.T) {
	// The summariser is told to write plain prose; this is the belt for when it
	// reaches for markdown anyway, since the result is read aloud.
	got := clampSummary("The **auth** work in `ws-client.ts`\n\nis nearly done.")
	if strings.ContainsAny(got, "*`") {
		t.Errorf("clampSummary() = %q, want no markdown punctuation", got)
	}
	if !strings.Contains(got, "auth") || !strings.Contains(got, "ws-client.ts") {
		t.Errorf("clampSummary() = %q, want the words kept", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("clampSummary() = %q, want a single spoken paragraph", got)
	}

	long := strings.Repeat("ä", maxSummaryOutput*2)
	bounded := clampSummary(long)
	if n := len([]rune(bounded)); n > maxSummaryOutput+1 {
		t.Errorf("kept %d runes, want <= %d plus an ellipsis", n, maxSummaryOutput+1)
	}
}

// A summariser with no model configured is off, and must cost nothing rather
// than failing a call.
func TestSummarizerDisabledIsSilent(t *testing.T) {
	s := newSessionSummarizer(nil, nil, "")
	if got := s.Summary(context.Background(), "sess-1"); got != "" {
		t.Errorf("Summary() = %q, want empty when disabled", got)
	}
	// Forget on a disabled (and on a nil) summariser must not panic: the turn
	// watcher calls it for every session, whether or not voice is configured.
	s.Forget("sess-1")
	var nilSummarizer *sessionSummarizer
	nilSummarizer.Forget("sess-1")
	if got := nilSummarizer.Summary(context.Background(), "sess-1"); got != "" {
		t.Errorf("nil Summary() = %q, want empty", got)
	}
}
