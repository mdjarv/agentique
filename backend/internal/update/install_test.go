package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		"AAAA1111  agentique-linux-amd64",
		"bbbb2222 *agentique-windows-amd64.exe", // binary-mode marker
		"",
		"garbage",
		"cccc3333  checksums.txt",
	}, "\n"))

	got, err := parseChecksums(in)
	if err != nil {
		t.Fatal(err)
	}
	if got["agentique-linux-amd64"] != "aaaa1111" {
		t.Errorf("digest not lowercased/parsed: %q", got["agentique-linux-amd64"])
	}
	if got["agentique-windows-amd64.exe"] != "bbbb2222" {
		t.Errorf("binary-mode '*' not stripped: %v", got)
	}
}

func TestParseChecksumsRejectsEmpty(t *testing.T) {
	if _, err := parseChecksums(strings.NewReader("\n\n")); err == nil {
		t.Fatal("an empty checksums file must be an error, not an empty map")
	}
}

func TestVerifyDigest(t *testing.T) {
	sums := map[string]string{"asset": "ABC"}
	if err := verifyDigest(sums, "asset", "abc"); err != nil {
		t.Errorf("case-insensitive compare failed: %v", err)
	}
	if err := verifyDigest(sums, "asset", "def"); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("mismatch must abort: %v", err)
	}
	if err := verifyDigest(sums, "missing", "abc"); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("an unpublished digest must abort: %v", err)
	}
}

func TestInstallOverKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentique")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installOver(src, target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "new")
	assertFile(t, target+PrevSuffix, "old")

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("installed binary is not executable")
	}
}

func TestInstallOverReplacesStalePrev(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentique")
	write(t, target, "v2")
	write(t, target+PrevSuffix, "v0") // left by an earlier upgrade
	src := filepath.Join(dir, "new")
	write(t, src, "v3")

	if err := installOver(src, target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "v3")
	// .prev must be the version we JUST replaced, not the ancient one.
	assertFile(t, target+PrevSuffix, "v2")
}

func TestRollbackSwapsBack(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentique")
	write(t, target, "new")
	write(t, target+PrevSuffix, "old")

	if err := Rollback(target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "old")
	// The rolled-back version becomes the new .prev, so rolling back twice
	// returns to it rather than doing nothing.
	assertFile(t, target+PrevSuffix, "new")

	if err := Rollback(target); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, "new")
}

func TestRollbackWithoutPrevious(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentique")
	write(t, target, "only")

	if err := Rollback(target); err == nil {
		t.Fatal("rollback with nothing to roll back to must fail")
	}
	assertFile(t, target, "only") // and must not have touched the binary
}

func TestDownloadToHashesAndCleansUpOnCancel(t *testing.T) {
	body := strings.Repeat("x", 512*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path, digest, err := downloadTo(context.Background(), srv.Client(), srv.URL, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest != onDisk {
		t.Fatalf("streamed digest %s != file digest %s", digest, onDisk)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadToCleansUpWhenCancelledMidStream(t *testing.T) {
	// A server that hands over a chunk and then stalls, so the cancel lands
	// with a temp file already open — the case that actually matters.
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 256*1024)))
		w.(http.Flusher).Flush()
		<-stall
	}))
	defer srv.Close()
	defer close(stall)

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Give the first chunk time to land before pulling the rug.
		for {
			entries, _ := filepath.Glob(filepath.Join(dir, ".agentique-update-*"))
			if len(entries) > 0 {
				cancel()
				return
			}
		}
	}()

	if _, _, err := downloadTo(ctx, srv.Client(), srv.URL, dir, nil); err == nil {
		t.Fatal("a cancelled download must fail")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".agentique-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("cancelled download left temp files: %v", leftovers)
	}
}

func TestDownloadToLandsBesideTheInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path, _, err := downloadTo(context.Background(), srv.Client(), srv.URL, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Same directory means same filesystem, which is what makes the install
	// rename atomic.
	if filepath.Dir(path) != dir {
		t.Fatalf("temp file landed in %s, want %s", filepath.Dir(path), dir)
	}
}

func TestWritableDirLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	if err := writableDir(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("write check left files behind: %v", entries)
	}

	readonly := filepath.Join(dir, "ro")
	if err := os.Mkdir(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := writableDir(readonly); err == nil {
		t.Fatal("an unwritable install dir must be detected before a button is offered")
	}
}

// --- helpers ---

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", filepath.Base(path), got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
