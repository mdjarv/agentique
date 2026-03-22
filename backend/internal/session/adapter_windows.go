//go:build windows

package session

import "fmt"

// NewRealConnector returns a CLIConnector stub on Windows.
// claudecli-go does not build on Windows due to Unix-specific syscall usage.
func NewRealConnector() CLIConnector {
	return &windowsStub{}
}

type windowsStub struct{}

func (w *windowsStub) Connect(workDir string) (CLISession, error) {
	return nil, fmt.Errorf("claudecli-go is not supported on Windows")
}

// ToWireEvent is a stub on Windows. Returns nil for all events.
func ToWireEvent(event CLIEvent) any {
	return nil
}
