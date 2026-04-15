// Package paths resolves platform-appropriate data directories for Agentique.
//
// Resolution order:
//  1. AGENTIQUE_HOME (explicit override)
//  2. XDG_DATA_HOME/agentique (XDG override, any platform)
//  3. Platform default:
//     - Linux:   ~/.local/share/agentique
//     - macOS:   ~/Library/Application Support/agentique
//     - Windows: %LOCALAPPDATA%/agentique
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "agentique"

// DataDir returns the root data directory for Agentique.
func DataDir() string {
	if v := os.Getenv("AGENTIQUE_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, appName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return appName // last resort: relative to CWD
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appName)
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, appName)
		}
		return filepath.Join(home, "AppData", "Local", appName)
	default: // linux, freebsd, etc.
		return filepath.Join(home, ".local", "share", appName)
	}
}

// DBPath returns the default database file path.
func DBPath() string {
	return filepath.Join(DataDir(), "agentique.db")
}

// WorktreeDir returns the base directory for git worktrees.
func WorktreeDir() string {
	return filepath.Join(DataDir(), "worktrees")
}

// SessionFilesDir returns the base directory for persistent session files (screenshots, exports).
// Each session gets a subdirectory: SessionFilesDir()/<session-id>/
func SessionFilesDir() string {
	return filepath.Join(DataDir(), "session-files")
}

// ConfigDir returns the configuration directory for Agentique.
//
// Resolution order:
//  1. AGENTIQUE_HOME (explicit override — config lives alongside data)
//  2. XDG_CONFIG_HOME/agentique
//  3. Platform default:
//     - Linux:   ~/.config/agentique
//     - macOS:   ~/Library/Application Support/agentique (same as data)
//     - Windows: %LOCALAPPDATA%/agentique (same as data)
func ConfigDir() string {
	if v := os.Getenv("AGENTIQUE_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, appName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return appName
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appName)
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, appName)
		}
		return filepath.Join(home, "AppData", "Local", appName)
	default:
		return filepath.Join(home, ".config", appName)
	}
}
