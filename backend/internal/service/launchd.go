package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const launchdLabel = "com.agentique.agent"

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
</dict>
</plist>
`))

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func launchdLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "agentique.log")
}

func installLaunchd(binaryPath string) error {
	path := plistPath()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	data := struct {
		Label      string
		BinaryPath string
		LogPath    string
	}{launchdLabel, binaryPath, launchdLogPath()}

	var buf bytes.Buffer
	if err := plistTemplate.Execute(&buf, data); err != nil {
		return fmt.Errorf("render plist template: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if out, err := exec.Command("launchctl", "load", "-w", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w\n%s", err, out)
	}

	return nil
}

func uninstallLaunchd() error {
	path := plistPath()

	exec.Command("launchctl", "unload", path).CombinedOutput()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	return nil
}

func statusLaunchd() (Status, error) {
	path := plistPath()
	s := Status{UnitPath: path}

	if _, err := os.Stat(path); err != nil {
		return s, nil
	}
	s.Installed = true

	out, err := exec.Command("launchctl", "list", launchdLabel).CombinedOutput()
	if err == nil {
		s.Running = true
		// Parse PID from first line: "PID\tStatus\tLabel" or "{" for JSON.
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			fields := strings.Fields(lines[0])
			if len(fields) > 0 {
				fmt.Sscanf(fields[0], "%d", &s.PID)
			}
		}
	}

	return s, nil
}

func restartLaunchd() error {
	path := plistPath()
	exec.Command("launchctl", "unload", path).CombinedOutput()
	if out, err := exec.Command("launchctl", "load", "-w", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w\n%s", err, out)
	}
	return nil
}

func startLaunchd() error {
	path := plistPath()
	if out, err := exec.Command("launchctl", "load", "-w", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w\n%s", err, out)
	}
	return nil
}

func stopLaunchd() error {
	path := plistPath()
	if out, err := exec.Command("launchctl", "unload", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl unload: %w\n%s", err, out)
	}
	return nil
}

func logsLaunchd() *exec.Cmd {
	return exec.Command("tail", "-f", launchdLogPath())
}
