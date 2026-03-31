// Package doctor checks that Agentique's runtime dependencies are present and healthy.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/allbin/agentique/backend/internal/paths"
	"github.com/allbin/agentique/backend/internal/update"
)

// Status is the outcome of a single check.
type Status int

const (
	OK   Status = iota
	Warn        // non-fatal, degraded functionality
	Fail        // fatal, cannot run
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	default:
		return "unknown"
	}
}

// Check is the result of a single dependency check.
type Check struct {
	Name     string
	Status   Status
	Message  string // human-readable detail
	Fix      string // how to fix (empty if OK)
	Required bool   // if true, Fail = server won't start
}

// Version is set by the caller (main package) so doctor can check for updates.
var Version string

// RunAll executes every check and returns results.
func RunAll() []Check {
	checks := []Check{
		checkClaude(),
		checkGit(),
		checkGH(),
		checkNode(),
		checkDataDir(),
		checkDiskSpace(),
		checkClaudeAuth(),
		checkGHAuth(),
	}
	if Version != "" && Version != "dev" {
		checks = append(checks, checkVersion())
	}
	return checks
}

// RunRequired returns only the checks needed for serve startup.
// These are fast checks (binary presence + version) — no network, no disk probing.
func RunRequired() []Check {
	return []Check{
		checkClaude(),
		checkGit(),
		checkGH(),
	}
}

// HasFailures reports whether any required check failed.
func HasFailures(checks []Check) bool {
	for _, c := range checks {
		if c.Required && c.Status == Fail {
			return true
		}
	}
	return false
}

// FormatError returns a combined error message for all required failures.
func FormatError(checks []Check) string {
	var parts []string
	for _, c := range checks {
		if c.Required && c.Status == Fail {
			s := c.Name + ": " + c.Message
			if c.Fix != "" {
				s += "\n  " + c.Fix
			}
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func checkClaude() Check {
	c := Check{Name: "claude", Required: true}

	path, err := exec.LookPath("claude")
	if err != nil {
		c.Status = Fail
		c.Message = "not found on PATH"
		c.Fix = platformFix("Install: npm install -g @anthropic-ai/claude-code",
			"", "") // no brew/pacman package for claude
		return c
	}

	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		c.Status = Fail
		c.Message = "failed to get version"
		c.Fix = "Verify: claude --version"
		return c
	}

	version := strings.TrimSpace(string(out))
	// Output: "2.1.87 (Claude Code)" — extract leading version
	major, _, ok := parseVersion(version)
	if !ok {
		c.Status = Warn
		c.Message = fmt.Sprintf("could not parse version %q", version)
		return c
	}

	if major < 2 {
		c.Status = Fail
		c.Message = fmt.Sprintf("version %s too old (need >= 2.0.0)", version)
		c.Fix = "Upgrade: npm install -g @anthropic-ai/claude-code"
		return c
	}

	c.Status = OK
	c.Message = version
	return c
}

func checkGit() Check {
	c := Check{Name: "git", Required: true}

	path, err := exec.LookPath("git")
	if err != nil {
		c.Status = Fail
		c.Message = "not found on PATH"
		c.Fix = platformFix("Install: https://git-scm.com/downloads", "git", "git")
		return c
	}

	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		c.Status = Fail
		c.Message = "failed to get version"
		return c
	}

	// "git version 2.53.0"
	version := strings.TrimSpace(string(out))
	version = strings.TrimPrefix(version, "git version ")

	c.Status = OK
	c.Message = version
	return c
}

func checkGH() Check {
	c := Check{Name: "gh", Required: false}

	path, err := exec.LookPath("gh")
	if err != nil {
		c.Status = Warn
		c.Message = "not found — PR creation will be unavailable"
		c.Fix = platformFix("Install: https://cli.github.com/", "gh", "github-cli")
		return c
	}

	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		c.Status = Warn
		c.Message = "failed to get version"
		return c
	}

	// "gh version 2.x.y (date)\nhttps://..."
	version := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	version = strings.TrimPrefix(version, "gh version ")

	c.Status = OK
	c.Message = version
	return c
}

func checkNode() Check {
	c := Check{Name: "node", Required: false}

	path, err := exec.LookPath("node")
	if err != nil {
		c.Status = Warn
		c.Message = "not found — needed if claude CLI requires update"
		c.Fix = platformFix("Install: https://nodejs.org/", "node", "nodejs")
		return c
	}

	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		c.Status = Warn
		c.Message = "failed to get version"
		return c
	}

	c.Status = OK
	c.Message = strings.TrimSpace(strings.TrimPrefix(string(out), "v"))
	return c
}

func checkDataDir() Check {
	c := Check{Name: "data-dir", Required: false}
	dir := paths.DataDir()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.Status = Fail
		c.Message = fmt.Sprintf("cannot create %s: %v", dir, err)
		c.Fix = "Check permissions on parent directory"
		c.Required = true
		return c
	}

	// Try writing a temp file to verify write access.
	f, err := os.CreateTemp(dir, ".doctor-check-*")
	if err != nil {
		c.Status = Fail
		c.Message = fmt.Sprintf("cannot write to %s: %v", dir, err)
		c.Fix = "Check directory permissions"
		c.Required = true
		return c
	}
	f.Close()
	os.Remove(f.Name())

	c.Status = OK
	c.Message = dir
	return c
}

func checkDiskSpace() Check {
	c := Check{Name: "disk-space", Required: false}
	dir := paths.DataDir()

	freeMB, err := freeSpaceMB(dir)
	if err != nil {
		c.Status = Warn
		c.Message = "could not check disk space"
		return c
	}

	if freeMB < 500 {
		c.Status = Warn
		c.Message = fmt.Sprintf("%d MB free in %s (recommend >= 500 MB)", freeMB, dir)
		return c
	}

	c.Status = OK
	c.Message = fmt.Sprintf("%d MB free", freeMB)
	return c
}

func checkClaudeAuth() Check {
	c := Check{Name: "claude-auth", Required: false}

	path, err := exec.LookPath("claude")
	if err != nil {
		c.Status = Warn
		c.Message = "skipped (claude not installed)"
		return c
	}

	out, err := exec.Command(path, "auth", "status").Output()
	if err != nil {
		c.Status = Warn
		c.Message = "not authenticated"
		c.Fix = "Run: claude auth login"
		return c
	}

	var auth struct {
		LoggedIn bool   `json:"loggedIn"`
		Email    string `json:"email"`
		OrgName  string `json:"orgName"`
	}
	if err := json.Unmarshal(out, &auth); err != nil || !auth.LoggedIn {
		c.Status = Warn
		c.Message = "not authenticated"
		c.Fix = "Run: claude auth login"
		return c
	}

	detail := auth.Email
	if auth.OrgName != "" {
		detail += " (" + auth.OrgName + ")"
	}
	c.Status = OK
	c.Message = detail
	return c
}

func checkGHAuth() Check {
	c := Check{Name: "gh-auth", Required: false}

	path, err := exec.LookPath("gh")
	if err != nil {
		c.Status = Warn
		c.Message = "skipped (gh not installed)"
		return c
	}

	// gh auth status exits 0 if logged in, 1 if not.
	out, err := exec.Command(path, "auth", "status").CombinedOutput()
	if err != nil {
		c.Status = Warn
		c.Message = "not authenticated — PR creation requires login"
		c.Fix = "Run: gh auth login"
		return c
	}

	// Parse account name from output: "Logged in to github.com account <name>"
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Logged in to") {
			// Extract "account <name>"
			if idx := strings.Index(line, "account "); idx >= 0 {
				account := strings.TrimSpace(line[idx+len("account "):])
				// Strip trailing parenthetical path.
				if paren := strings.Index(account, " ("); paren >= 0 {
					account = account[:paren]
				}
				c.Status = OK
				c.Message = account
				return c
			}
		}
	}

	c.Status = OK
	c.Message = "authenticated"
	return c
}

func checkVersion() Check {
	c := Check{Name: "version", Required: false}
	c.Message = Version

	latest, err := fetchLatestVersion()
	if err != nil {
		// Network failure is not worth warning about.
		c.Status = OK
		return c
	}

	if update.SemverNewer(latest, Version) {
		c.Status = Warn
		c.Message = fmt.Sprintf("%s (latest: %s)", Version, latest)
		c.Fix = "Upgrade: agentique upgrade"
		return c
	}

	c.Status = OK
	c.Message = Version + " (up to date)"
	return c
}

func fetchLatestVersion() (string, error) {
	// Use gh CLI if available — handles auth, avoids rate limits.
	if ghPath, err := exec.LookPath("gh"); err == nil {
		out, err := exec.Command(ghPath, "api", "repos/allbin/agentique/releases/latest", "--jq", ".tag_name").Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
	}

	// Fallback: unauthenticated curl.
	out, err := exec.Command("curl", "-fsSL", "--max-time", "5",
		"https://api.github.com/repos/allbin/agentique/releases/latest").Output()
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

func hasHomebrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func isArch() bool {
	_, err := exec.LookPath("pacman")
	return err == nil
}

func platformFix(generic string, brewPkg string, archPkg string) string {
	if runtime.GOOS == "darwin" && hasHomebrew() && brewPkg != "" {
		return fmt.Sprintf("Install: brew install %s", brewPkg)
	}
	if runtime.GOOS == "linux" && isArch() && archPkg != "" {
		return fmt.Sprintf("Install: pacman -S %s", archPkg)
	}
	return generic
}

// parseVersion extracts major, minor from a version string like "2.1.87 (Claude Code)".
func parseVersion(s string) (major, minor int, ok bool) {
	// Take first space-delimited token.
	token := strings.Fields(s)
	if len(token) == 0 {
		return 0, 0, false
	}
	parts := strings.SplitN(token[0], ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return maj, min, true
}
