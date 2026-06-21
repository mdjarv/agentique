//go:build !darwin || cgo

package main

import (
	_ "embed"
	"log/slog"
	"runtime"
	"time"

	"fyne.io/systray"
)

//go:embed trayicon/icon.png
var iconPNG []byte

//go:embed trayicon/icon.ico
var iconICO []byte

// trayIcon returns the platform-appropriate icon bytes: a real ICO on Windows,
// PNG elsewhere.
func trayIcon() []byte {
	if runtime.GOOS == "windows" {
		return iconICO
	}
	return iconPNG
}

func runTray() error {
	// systray.Run blocks until Quit and must run on the main goroutine (which
	// cobra's RunE is). onExit is a no-op: we leave the server running.
	systray.Run(onTrayReady, func() {})
	return nil
}

func onTrayReady() {
	systray.SetIcon(trayIcon())
	systray.SetTooltip("agentique")

	mStatus := systray.AddMenuItem("Status: …", "Server status")
	mStatus.Disable()
	mOpen := systray.AddMenuItem("Open agentique", "Open the web UI in your browser")
	systray.AddSeparator()
	mStart := systray.AddMenuItem("Start server", "Start the agentique server")
	mStop := systray.AddMenuItem("Stop server", "Stop the agentique server")
	mRestart := systray.AddMenuItem("Restart server", "Restart the agentique server")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit the tray (the server keeps running)")

	refresh := func() {
		if isServerRunning() {
			mStatus.SetTitle("● Running · " + displayAddr())
			systray.SetTooltip("agentique — running")
			mStart.Disable()
			mStop.Enable()
			mRestart.Enable()
		} else {
			mStatus.SetTitle("○ Stopped")
			systray.SetTooltip("agentique — stopped")
			mStart.Enable()
			mStop.Disable()
			mRestart.Disable()
		}
	}
	refresh()

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refresh()
			case <-mOpen.ClickedCh:
				logIfErr("open UI", openUI())
			case <-mStart.ClickedCh:
				logIfErr("start server", startServer())
				waitThenRefresh(refresh, true)
			case <-mStop.ClickedCh:
				logIfErr("stop server", stopServer())
				waitThenRefresh(refresh, false)
			case <-mRestart.ClickedCh:
				logIfErr("restart server", restartServer())
				waitThenRefresh(refresh, true)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// waitThenRefresh polls briefly for the server to reach the desired state, then
// updates the menu — so the UI reflects the action without waiting a full tick.
func waitThenRefresh(refresh func(), wantRunning bool) {
	for range 20 {
		if isServerRunning() == wantRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	refresh()
}

func logIfErr(action string, err error) {
	if err != nil {
		slog.Warn("tray: "+action+" failed", "error", err)
	}
}
