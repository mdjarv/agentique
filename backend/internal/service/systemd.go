package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

const serviceName = "agentique"

var unitTemplate = template.Must(template.New("unit").Parse(`[Unit]
Description=Agentique — Claude Code Agent Manager
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`))

func unitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", serviceName+".service")
}

func installSystemd(binaryPath string) error {
	path := unitPath()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create unit directory: %w", err)
	}

	var buf bytes.Buffer
	if err := unitTemplate.Execute(&buf, struct{ BinaryPath string }{binaryPath}); err != nil {
		return fmt.Errorf("render unit template: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", serviceName},
	}
	for _, args := range cmds {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}

	return nil
}

func uninstallSystemd() error {
	path := unitPath()

	cmds := [][]string{
		{"systemctl", "--user", "disable", "--now", serviceName},
	}
	for _, args := range cmds {
		// Ignore errors — service might not be running.
		exec.Command(args[0], args[1:]...).CombinedOutput()
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}

	exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput()
	return nil
}

func statusSystemd() (Status, error) {
	path := unitPath()
	s := Status{UnitPath: path}

	if _, err := os.Stat(path); err != nil {
		return s, nil // not installed
	}
	s.Installed = true

	out, err := exec.Command("systemctl", "--user", "is-active", serviceName).Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		s.Running = true
	}

	out, err = exec.Command("systemctl", "--user", "show", serviceName, "-p", "MainPID", "--value").Output()
	if err == nil {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		s.PID = pid
	}

	return s, nil
}

func logsSystemd() *exec.Cmd {
	return exec.Command("journalctl", "--user", "-u", serviceName, "-f", "--no-pager")
}
