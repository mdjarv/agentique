package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ChecksumsAsset is the file every release publishes alongside the binaries.
const ChecksumsAsset = "checksums.txt"

// PrevSuffix names the binary we keep so `agentique rollback` has something to
// swap back to. Nothing auto-reverts — an automatic rollback that also fails is
// a worse place to be.
const PrevSuffix = ".prev"

// ErrChecksumMismatch is what makes this feature safe to have at all: a
// downloaded binary that does not match the published digest is never
// installed.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// parseChecksums reads `sha256␠␠name` lines (sha256sum output; a leading `*`
// on the name marks binary mode) into name → digest.
func parseChecksums(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		out[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("checksums file had no entries")
	}
	return out, nil
}

// fetchChecksums downloads and parses the release's checksums.txt.
func fetchChecksums(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksums returned %d", resp.StatusCode)
	}
	return parseChecksums(io.LimitReader(resp.Body, 1<<20))
}

// downloadTo streams url into a new temp file **beside** dir — same
// filesystem, so the rename that installs it is atomic — hashing as it goes.
// onBytes is called as the download advances. The caller owns the returned
// path and must remove it unless it installs it.
func downloadTo(ctx context.Context, client *http.Client, url, dir string, onBytes func(done, total int64)) (path string, digest string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, ".agentique-update-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	// Held in a local, NOT the named return: a `return "", "", err` blanks the
	// named one before the deferred cleanup can read it, and the temp file
	// survives the very failure it was supposed to be cleaned up by.
	tmpPath := tmp.Name()
	defer func() {
		cerr := tmp.Close()
		if err != nil {
			// Failure or cancel: nothing is installed, so leave nothing behind.
			err = errors.Join(err, os.Remove(tmpPath))
			return
		}
		err = cerr
	}()

	h := sha256.New()
	if err = copyWithProgress(ctx, io.MultiWriter(tmp, h), resp.Body, resp.ContentLength, onBytes); err != nil {
		return "", "", err
	}
	return tmpPath, hex.EncodeToString(h.Sum(nil)), nil
}

// copyWithProgress is io.Copy that reports as it goes and stops when the
// context is cancelled — the download is the long phase, so it is where cancel
// has to actually work.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, onBytes func(done, total int64)) error {
	buf := make([]byte, 128*1024)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return fmt.Errorf("write download: %w", werr)
			}
			done += int64(n)
			if onBytes != nil {
				onBytes(done, total)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("read download: %w", rerr)
		}
	}
}

// verifyDigest compares what we downloaded against what the release published.
// A mismatch aborts — no path installs an unverified binary.
func verifyDigest(checksums map[string]string, asset, got string) error {
	want, ok := checksums[asset]
	if !ok {
		return fmt.Errorf("%w: no published digest for %s", ErrChecksumMismatch, asset)
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("%w: %s expected %s, got %s", ErrChecksumMismatch, asset, want, got)
	}
	return nil
}

// installOver puts src in place of target, keeping the old binary as
// target.prev. Both steps are renames within one directory: on POSIX the
// running process keeps its open inode, and on Windows a running .exe can be
// renamed aside even though it cannot be overwritten.
func installOver(src, target string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}
	prev := target + PrevSuffix
	// A leftover .prev from a previous upgrade is expected; it is about to be
	// replaced by the version we are actually leaving behind.
	if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear previous backup: %w", err)
	}
	if err := os.Rename(target, prev); err != nil {
		return fmt.Errorf("keep current binary as %s: %w", filepath.Base(prev), err)
	}
	if err := os.Rename(src, target); err != nil {
		// Put the old binary back rather than leaving the install dir with no
		// agentique in it at all.
		return errors.Join(fmt.Errorf("install new binary: %w", err), os.Rename(prev, target))
	}
	return nil
}

// Rollback swaps target.prev back over target. Deliberate and manual: nothing
// auto-reverts.
func Rollback(target string) error {
	prev := target + PrevSuffix
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("no previous binary at %s: %w", prev, err)
	}
	// Swap through a temp name so the version being rolled back is still
	// recoverable — and so a failure never leaves the target missing.
	staging := target + ".rollback"
	if err := os.Remove(staging); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear staging path: %w", err)
	}
	if err := os.Rename(target, staging); err != nil {
		return fmt.Errorf("move current aside: %w", err)
	}
	if err := os.Rename(prev, target); err != nil {
		return errors.Join(fmt.Errorf("restore previous binary: %w", err), os.Rename(staging, target))
	}
	// The version we just rolled back becomes the new .prev, so a second
	// rollback returns to it rather than doing nothing.
	if err := os.Rename(staging, prev); err != nil {
		return fmt.Errorf("keep rolled-back binary: %w", err)
	}
	return nil
}

// writableDir reports whether we could actually create the temp file the
// install needs. Checked before a button is offered, not after it is pressed.
func writableDir(dir string) error {
	f, err := os.CreateTemp(dir, ".agentique-write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	return errors.Join(f.Close(), os.Remove(name))
}

// hashFile is the digest of an on-disk file, for tests and the CLI.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var h hash.Hash = sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
