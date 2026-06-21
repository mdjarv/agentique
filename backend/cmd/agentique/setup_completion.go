package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// detectShell returns "bash", "zsh", "fish", or "powershell". It prefers the
// SHELL env var (set on Unix and under Git Bash); on Windows, where SHELL is
// usually unset, it falls back to PowerShell. Returns "" if unrecognized.
func detectShell() string {
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "bash", "zsh", "fish":
		return shell
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return ""
}

// installCompletion generates and installs shell completions for the detected shell.
// Returns a human-readable detail string on success.
func installCompletion(shell string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable: %w", err)
	}

	if shell == "powershell" {
		return installPowerShellCompletion(exe)
	}

	var destPath string
	switch shell {
	case "fish":
		destPath = filepath.Join(homeDir(), ".config", "fish", "completions", "agentique.fish")
	case "zsh":
		// Try to find a writable fpath directory, fall back to ~/.zsh/completions.
		destPath = findZshCompletionDir()
	case "bash":
		destPath = filepath.Join(homeDir(), ".local", "share", "bash-completion", "completions", "agentique")
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	out, err := exec.Command(exe, "completion", shell).Output()
	if err != nil {
		return "", fmt.Errorf("generate completions: %w", err)
	}

	if err := os.WriteFile(destPath, out, 0o644); err != nil {
		return "", fmt.Errorf("write completions: %w", err)
	}

	return destPath, nil
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// installPowerShellCompletion writes the generated completion script under the
// config dir and ensures the user's PowerShell profile dot-sources it. Unlike
// the Unix shells there is no auto-loaded per-command directory, so wiring the
// profile is how completions take effect.
func installPowerShellCompletion(exe string) (string, error) {
	out, err := exec.Command(exe, "completion", "powershell").Output()
	if err != nil {
		return "", fmt.Errorf("generate completions: %w", err)
	}

	scriptPath := filepath.Join(paths.ConfigDir(), "completions", "agentique.ps1")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(scriptPath, out, 0o644); err != nil {
		return "", fmt.Errorf("write completions: %w", err)
	}

	profile, err := powerShellProfilePath()
	if err != nil {
		// Script is in place; we just couldn't locate the profile to wire it up.
		return fmt.Sprintf("%s (add `. '%s'` to your PowerShell $PROFILE to enable)", scriptPath, scriptPath), nil
	}
	if err := ensureProfileSources(profile, scriptPath); err != nil {
		return "", err
	}
	return scriptPath, nil
}

// powerShellProfilePath asks PowerShell for the current-user/current-host
// profile path, which respects Documents-folder redirection (e.g. OneDrive).
func powerShellProfilePath() (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "$PROFILE.CurrentUserCurrentHost").Output()
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("empty profile path")
	}
	return p, nil
}

// ensureProfileSources idempotently appends a dot-source line for scriptPath to
// the PowerShell profile, creating the profile (and its directory) if needed.
func ensureProfileSources(profile, scriptPath string) error {
	if existing, err := os.ReadFile(profile); err == nil && strings.Contains(string(existing), scriptPath) {
		return nil // already wired up
	}
	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	f, err := os.OpenFile(profile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open profile: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n# agentique completions\n. '%s'\n", scriptPath); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

func findZshCompletionDir() string {
	// Check if any fpath directory under home is writable.
	fpath := os.Getenv("FPATH")
	if fpath != "" {
		home := homeDir()
		for _, dir := range strings.Split(fpath, ":") {
			if strings.HasPrefix(dir, home) {
				if info, err := os.Stat(dir); err == nil && info.IsDir() {
					return filepath.Join(dir, "_agentique")
				}
			}
		}
	}
	// Fallback: ~/.zsh/completions (common user-local convention).
	return filepath.Join(homeDir(), ".zsh", "completions", "_agentique")
}
