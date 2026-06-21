//go:build darwin && !cgo

package main

import "errors"

// runTray is unavailable on macOS without cgo — the systray backend links
// against Cocoa. Build with CGO_ENABLED=1 to get the tray, or use the server /
// service directly.
func runTray() error {
	return errors.New("the tray requires a cgo-enabled macOS build (CGO_ENABLED=1) — use 'agentique serve' or 'agentique service install' instead")
}
