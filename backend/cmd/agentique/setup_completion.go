package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// detectShell returns "bash", "zsh", or "fish" based on SHELL env var.
// Returns empty string if unrecognized.
func detectShell() string {
	shell := filepath.Base(os.Getenv("SHELL"))
	switch shell {
	case "bash", "zsh", "fish":
		return shell
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
