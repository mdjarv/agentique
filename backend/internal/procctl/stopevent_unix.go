//go:build !windows

package procctl

// NotifyStopRequests is a no-op on unix: stop requests are SIGTERM, which the
// serve loop already listens for. The nil channel never fires in a select, so
// callers need no platform branch.
func NotifyStopRequests(dataDir string) (<-chan struct{}, error) { return nil, nil }

// RequestStop always reports ErrNoStopListener on unix; callers fall back to
// Terminate, which is SIGTERM here — the graceful path already.
func RequestStop(dataDir string) error { return ErrNoStopListener }
