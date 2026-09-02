//go:build windows

package procctl

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// NotifyStopRequests creates the data dir's named stop event and returns a
// channel that is closed when some other process signals it (RequestStop).
// This is the Windows stand-in for SIGTERM: a Scheduled Task has no way to
// deliver a signal, so `agentique service stop` and the tray set this event and
// the serve loop treats it exactly like an interrupt.
//
// The event is reset after creation: a stopper's still-open handle can keep a
// previous instance's signaled event alive across our restart, and starting up
// pre-stopped would read as a server that dies immediately for no reason.
func NotifyStopRequests(dataDir string) (<-chan struct{}, error) {
	name, err := windows.UTF16PtrFromString(stopEventName(dataDir))
	if err != nil {
		return nil, fmt.Errorf("stop event name: %w", err)
	}
	// Auto-reset (manualReset=0), initially unsignaled.
	h, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		return nil, fmt.Errorf("create stop event: %w", err)
	}
	if err := windows.ResetEvent(h); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("reset stop event: %w", err)
	}

	ch := make(chan struct{})
	go func() {
		// Blocks for the life of the process unless signaled; the handle is
		// intentionally held open so the name stays claimed while we run.
		if ev, err := windows.WaitForSingleObject(h, windows.INFINITE); err == nil && ev == windows.WAIT_OBJECT_0 {
			close(ch)
		}
	}()
	return ch, nil
}

// RequestStop asks the server owning dataDir to shut down gracefully by
// signaling its stop event. ErrNoStopListener when no such event exists —
// nothing is running, or the running build predates the mechanism.
func RequestStop(dataDir string) error {
	name, err := windows.UTF16PtrFromString(stopEventName(dataDir))
	if err != nil {
		return fmt.Errorf("stop event name: %w", err)
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoStopListener, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.SetEvent(h); err != nil {
		return fmt.Errorf("signal stop event: %w", err)
	}
	return nil
}
