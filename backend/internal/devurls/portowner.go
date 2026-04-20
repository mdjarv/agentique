package devurls

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PortOwner describes the process currently listening on a TCP port.
// Populated from /proc — Linux-only. Fields may be empty if /proc lookup
// partially fails (e.g. owner is in a different user's namespace).
type PortOwner struct {
	PID       int
	Cmdline   string
	Cwd       string
	Exe       string
	StartedAt time.Time
}

// ProbePort returns nil if the port can be bound on 127.0.0.1, or an error if
// already in use. Callers should treat any non-nil result as "busy".
func ProbePort(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	return l.Close()
}

// FindPortOwner returns the process listening on the given TCP port, or nil
// if no listener is found. Returns nil, nil for "no owner" so callers can
// distinguish "port free" from "/proc unavailable".
func FindPortOwner(port int) (*PortOwner, error) {
	inode, err := findListenInode(port)
	if err != nil {
		return nil, err
	}
	if inode == 0 {
		return nil, nil
	}
	pid, err := findPIDByInode(inode)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}
	return readProcess(pid), nil
}

func findListenInode(port int) (uint64, error) {
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		inode, err := scanProcNet(path, port)
		if err != nil {
			continue
		}
		if inode != 0 {
			return inode, nil
		}
	}
	return 0, nil
}

func scanProcNet(path string, port int) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return 0, nil
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		// state "0A" == TCP_LISTEN.
		if fields[3] != "0A" {
			continue
		}
		localAddr := fields[1]
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 {
			continue
		}
		p, err := strconv.ParseInt(localAddr[idx+1:], 16, 32)
		if err != nil {
			continue
		}
		if int(p) != port {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		return inode, nil
	}
	return 0, nil
}

func findPIDByInode(inode uint64) (int, error) {
	target := fmt.Sprintf("socket:[%d]", inode)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if link == target {
				return pid, nil
			}
		}
	}
	return 0, nil
}

func readProcess(pid int) *PortOwner {
	owner := &PortOwner{PID: pid}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		owner.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
	}
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		owner.Cwd = cwd
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		owner.Exe = exe
	}
	// /proc/<pid> ctime/mtime reflect process start on Linux.
	if stat, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		owner.StartedAt = stat.ModTime()
	}
	return owner
}

// Describe returns a single-line human-readable summary of the owner.
func (o *PortOwner) Describe() string {
	if o == nil {
		return "unknown"
	}
	parts := []string{fmt.Sprintf("pid=%d", o.PID)}
	if o.Cmdline != "" {
		parts = append(parts, fmt.Sprintf("cmd=%q", truncate(o.Cmdline, 120)))
	}
	if o.Cwd != "" {
		parts = append(parts, fmt.Sprintf("cwd=%s", o.Cwd))
	}
	if !o.StartedAt.IsZero() {
		parts = append(parts, fmt.Sprintf("started=%s ago", time.Since(o.StartedAt).Truncate(time.Second)))
	}
	return strings.Join(parts, " ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
