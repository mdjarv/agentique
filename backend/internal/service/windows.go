package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// Windows has no per-user service manager equivalent to systemd --user or a
// launchd LaunchAgent, so we model the service as a Scheduled Task with a logon
// trigger and restart-on-failure. This runs as the current interactive user
// (LeastPrivilege / InteractiveToken), so installation needs no admin elevation
// — matching the per-user semantics of the other platforms. A true Windows
// Service (SCM) would require admin rights and SCM dispatch integration in the
// serve command, neither of which fits the existing model.

const taskName = "agentique"

// taskDisplayPath is how Task Scheduler identifies a root-level task.
const taskDisplayPath = `\agentique`

var taskTemplate = template.Must(template.New("task").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Agentique — Claude Code Agent Manager</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>{{.UserID}}</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>{{.UserID}}</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{.BinaryPath}}</Command>
      <Arguments>serve</Arguments>
    </Exec>
  </Actions>
</Task>
`))

func windowsLogPath() string {
	return filepath.Join(paths.DataDir(), "agentique.log.jsonl")
}

// currentUserID returns DOMAIN\user (or just user) for the task principal.
func currentUserID() string {
	user := os.Getenv("USERNAME")
	if domain := os.Getenv("USERDOMAIN"); domain != "" && user != "" {
		return domain + `\` + user
	}
	return user
}

func installWindows(binaryPath string) error {
	var buf strings.Builder
	data := struct {
		BinaryPath string
		UserID     string
	}{
		BinaryPath: xmlEscape(binaryPath),
		UserID:     xmlEscape(currentUserID()),
	}
	if err := taskTemplate.Execute(&buf, data); err != nil {
		return fmt.Errorf("render task definition: %w", err)
	}

	tmp, err := os.CreateTemp("", "agentique-task-*.xml")
	if err != nil {
		return fmt.Errorf("create task definition file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(buf.String()); err != nil {
		tmp.Close()
		return fmt.Errorf("write task definition: %w", err)
	}
	tmp.Close()

	if out, err := exec.Command("schtasks", "/Create", "/TN", taskName, "/XML", tmp.Name(), "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Create: %w\n%s", err, out)
	}

	// Start only if not already running — never restart behind the user's back.
	if s, _ := statusWindows(); !s.Running {
		if out, err := exec.Command("schtasks", "/Run", "/TN", taskName).CombinedOutput(); err != nil {
			return fmt.Errorf("schtasks /Run: %w\n%s", err, out)
		}
	}
	return nil
}

func uninstallWindows() error {
	// Best effort stop; the task may not be running.
	exec.Command("schtasks", "/End", "/TN", taskName).CombinedOutput()

	if out, err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").CombinedOutput(); err != nil {
		// Deleting a non-existent task is not an error for our purposes.
		if strings.Contains(strings.ToLower(string(out)), "cannot find") {
			return nil
		}
		return fmt.Errorf("schtasks /Delete: %w\n%s", err, out)
	}
	return nil
}

func statusWindows() (Status, error) {
	s := Status{UnitPath: taskDisplayPath}

	out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "LIST", "/V").CombinedOutput()
	if err != nil {
		return s, nil // not installed
	}
	s.Installed = true

	for _, line := range strings.Split(string(out), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Status") {
			if strings.EqualFold(strings.TrimSpace(val), "Running") {
				s.Running = true
			}
		}
	}
	return s, nil
}

func restartWindows() error {
	exec.Command("schtasks", "/End", "/TN", taskName).CombinedOutput()
	if out, err := exec.Command("schtasks", "/Run", "/TN", taskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Run: %w\n%s", err, out)
	}
	return nil
}

func startWindows() error {
	if out, err := exec.Command("schtasks", "/Run", "/TN", taskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Run: %w\n%s", err, out)
	}
	return nil
}

func stopWindows() error {
	if out, err := exec.Command("schtasks", "/End", "/TN", taskName).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /End: %w\n%s", err, out)
	}
	return nil
}

// logsWindows tails the JSON log file the server writes in file/auto mode
// (the default on Windows). Task Scheduler has no centralized log stream.
func logsWindows() *exec.Cmd {
	return exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Get-Content -LiteralPath '%s' -Wait -Tail 50", windowsLogPath()))
}

// xmlEscape escapes a value for inclusion in the task XML text content.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
