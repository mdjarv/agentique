// Package doctor checks that Agentique's runtime dependencies are present and healthy.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudeadapter "github.com/allbin/agentkit/runtime/cli/claude"
	codexadapter "github.com/allbin/agentkit/runtime/cli/codex"
	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/paths"
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

// CheckFunc pairs a check name with its function for sequential execution.
type CheckFunc struct {
	Name string
	Run  func() Check
}

// AllChecks returns all checks as individual functions.
// Use this when you need to run checks one at a time (e.g. animated UI).
func AllChecks() []CheckFunc {
	return []CheckFunc{
		{"claude", checkClaude},
		{"git", checkGit},
		{"gh", checkGH},
		{"node", checkNode},
		{"data-dir", checkDataDir},
		{"disk-space", checkDiskSpace},
		{"claude-auth", checkClaudeAuth},
		{"gh-auth", checkGHAuth},
		{"server-addr", checkServerAddr},
	}
}

// RunAll executes every check and returns results.
func RunAll() []Check {
	return []Check{
		checkClaude(),
		checkCodex(),
		checkGit(),
		checkGH(),
		checkNode(),
		checkDataDir(),
		checkDiskSpace(),
		checkClaudeAuth(),
		checkGHAuth(),
		checkServerAddr(),
	}
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

// checkClaude reports the claude CLI the *server* would spawn, and the command
// that would update that install.
//
// It never resolves the binary itself and never runs the CLI: detection comes
// from the provider's own library through agentkit's connector capability, so
// this and the Versions dialog cannot disagree (docs/upgrades.md C1, C10, C13).
//
// The Fix line is whatever the library says, or nothing. It used to hardcode
// `npm install -g @anthropic-ai/claude-code`, which on a native install writes
// a second copy into an npm prefix and leaves the install it was describing
// untouched — the user is then shown a version that does not describe the
// binary their next session runs.
func checkClaude() Check {
	c := Check{Name: "claude", Required: true}

	info, err := detectCLI("claude")
	if err != nil {
		c.Status = Fail
		c.Message = "not found on PATH"
		c.Fix = "Install: https://claude.com/product/claude-code"
		return c
	}

	c.Status = OK
	c.Message = describeInstall(info)
	if major, _, ok := parseVersion(info.Version); ok && major < 2 {
		c.Status = Fail
		c.Message = fmt.Sprintf("version %s too old (need >= 2.0.0)", info.Version)
		c.Fix = updateHint(info)
	}
	return c
}

// checkCodex reports the codex CLI, when one is installed. Optional: codex is
// a second provider, not a dependency, so its absence is not a warning either.
func checkCodex() Check {
	c := Check{Name: "codex", Required: false}

	info, err := detectCLI("codex")
	if err != nil {
		c.Status = OK
		c.Message = "not installed (optional)"
		return c
	}

	c.Status = OK
	c.Message = describeInstall(info)
	return c
}

// detectCLI asks the provider's connector how its CLI was installed. The
// connectors are constructed with no binary override, exactly as server.New
// constructs them, so the answer describes the binary a session would spawn.
func detectCLI(tool string) (*runtime.Install, error) {
	var conn runtime.CLIConnector
	switch tool {
	case "claude":
		conn = claudeadapter.NewConnector()
	case "codex":
		conn = codexadapter.NewConnector()
	default:
		return nil, fmt.Errorf("doctor: unknown provider %q", tool)
	}
	in, ok := conn.(runtime.InstallInspectable)
	if !ok {
		return nil, fmt.Errorf("doctor: %s connector cannot report its install", tool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliDetectTimeout)
	defer cancel()
	info, err := in.InstallInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("doctor: detect %s: %w", tool, err)
	}
	return info, nil
}

// cliDetectTimeout bounds detection, which spawns one `--version`.
const cliDetectTimeout = 10 * time.Second

// describeInstall renders "2.1.241 (native)" plus anything the library thought
// worth warning about — a second copy on PATH is the one that matters, since
// that is when a version stops describing the binary that runs.
func describeInstall(info *runtime.Install) string {
	version := info.Version
	if version == "" {
		version = "unknown version"
	}
	msg := version
	if info.Method != "" && info.Method != runtime.InstallMethodUnknown {
		msg += " (" + info.Method + ")"
	}
	if len(info.Warnings) > 0 {
		msg += " — " + strings.Join(info.Warnings, "; ")
	}
	return msg
}

// updateHint is the library's own update command, or an honest shrug. An empty
// UpdateCmd means "update manually"; it never means "use npm".
func updateHint(info *runtime.Install) string {
	if info.UpdateCmd == "" {
		return "Update it the way it was installed — no command is known to be correct for a " + info.Method + " install"
	}
	return "Upgrade: " + info.UpdateCmd
}

func checkGit() Check {
	c := Check{Name: "git", Required: true}

	path, err := exec.LookPath("git")
	if err != nil {
		c.Status = Fail
		c.Message = "not found on PATH"
		c.Fix = "Install git: https://git-scm.com/downloads"
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
		c.Fix = "Install: https://cli.github.com/"
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
		c.Fix = "Install: https://nodejs.org/"
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

// checkClaudeAuth asks claudecli-go rather than running `claude auth status`
// and parsing the JSON here — same rule as checkClaude: the library owns the
// command. It also gets defensive parsing and a three-state answer for free.
func checkClaudeAuth() Check {
	c := Check{Name: "claude-auth", Required: false}

	ctx, cancel := context.WithTimeout(context.Background(), cliDetectTimeout)
	defer cancel()
	auth, err := claudecli.AuthStatus(ctx)
	if err != nil || auth == nil || !auth.LoggedIn {
		c.Status = Warn
		c.Message = "not authenticated"
		c.Fix = "Run: claude auth login"
		return c
	}

	detail := auth.Email
	if auth.OrgName != "" {
		detail += " (" + auth.OrgName + ")"
	}
	if detail == "" {
		detail = "authenticated"
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

func checkServerAddr() Check {
	c := Check{Name: "server-addr", Required: false}

	cfg, err := config.Load(config.Path())
	if err != nil {
		c.Status = Warn
		c.Message = fmt.Sprintf("could not load config: %v", err)
		c.Fix = "Check " + config.Path()
		return c
	}

	scheme := "http"
	if cfg.Server.TLSCert != "" && cfg.Server.TLSKey != "" {
		scheme = "https"
	}

	source := "default"
	if _, err := os.Stat(config.Path()); err == nil {
		source = config.Path()
	}

	c.Status = OK
	c.Message = fmt.Sprintf("%s://%s (%s)", scheme, cfg.Server.Addr, source)
	return c
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
