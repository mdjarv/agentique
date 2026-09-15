package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/allbin/agentkit/eventbus"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// The assistant's head, started the way a sessionless persona is.
//
// It is a Claude CLI subprocess with no sessions row, no worktree and no
// project — the conversation is the memory and the head's context is a cache of
// it, so a restart costs the turn and nothing else. What it does have that a
// discussion persona does not is TOOLS: the verb table reaches it over an MCP
// endpoint of its own, authenticated by a bearer minted for the head's own id
// in a token store nothing else writes to.
//
// That bearer is the reason this adapter exists at all rather than the core
// calling StartPersonaRuntime itself. The token has to be in the store BEFORE
// the subprocess starts, which means the caller has to choose the id; and it
// reaches the CLI as a path to a 0600 FILE, never inline, because
// /proc/<pid>/cmdline is world-readable and inline JSON would put the credential
// in argv for every local user to read.
//
// This is also where the head's containment is actually built, because this is
// where its subprocess is. The verb table is what the assistant may DO, and it
// is worth nothing while the CLI underneath it carries a shell: the head runs
// fullAuto (there is no screen to approve anything on) and it is fed untrusted
// agent text on every single turn, as news and as reports. So the native tools
// are denied here and the working directory is a scratch directory of its own.
// internal/assistant cannot spell either: it is provider-neutral, and the tool
// names are claude's.

// assistantMCPPath is where the head's own MCP endpoint is mounted, under the
// session endpoint's path rather than beside it: both are outside /api/ and both
// authenticate by bearer against a token store, so one prefix is one rule to
// remember in the security headers and in anything that reasons about /mcp.
const assistantMCPPath = "/mcp/assistant"

// assistantMCPURL turns the session endpoint's internal URL into the head's.
//
// Derived rather than configured: there is one listener, and a second setting
// for the same port is a second thing to get wrong. An empty base stays empty —
// that is "the endpoint is not wired", and the head then runs with no tools.
func assistantMCPURL(base string) string {
	if base == "" {
		return ""
	}
	return base + "/assistant"
}

// headDisallowedTools are the provider-native tools the head must not have.
//
// Everything that reads or writes the filesystem, runs a command, or reaches
// the network. The head has no project, no worktree and no repository to read,
// so none of them is a capability it is losing — where the data directory it
// would be sitting beside holds every paired machine's outbound bearer, and
// CLAUDE.md is explicit that the data dir is not protected from an agent.
//
// Task goes too: a subagent is a second context with its own tool set, and a
// deny list that a spawn can step around is not one.
var headDisallowedTools = []string{
	"Bash", "BashOutput", "KillShell", "KillBash",
	"Read", "Write", "Edit", "MultiEdit", "NotebookEdit",
	"Glob", "Grep",
	"WebFetch", "WebSearch",
	"Task", "SlashCommand",
}

// assistantHeads starts the assistant's head. It implements
// assistant.HeadManager.
type assistantHeads struct {
	mgr    *session.Manager
	tokens *mcphttp.TokenStore
	// mcpURL is where the head's subprocess reaches its OWN MCP endpoint —
	// not the one every coding session uses. Empty means the endpoint is not
	// wired, and the head then runs with no tools rather than not at all — it
	// can still talk, which is better than an assistant that cannot be spoken
	// to.
	mcpURL string
}

// StartHead implements assistant.HeadManager.
func (a *assistantHeads) StartHead(ctx context.Context, p assistant.HeadParams) (assistant.HeadRuntime, error) {
	if a.mgr == nil {
		return nil, fmt.Errorf("no session manager, so there is nothing to start a head with")
	}

	workDir, err := headWorkDir()
	if err != nil {
		// Unlike the MCP credential, this does not degrade: a head with no
		// working directory of its own inherits the server process's, which is
		// wherever the operator ran the service from. That is a containment
		// claim this file is making, not a nicety.
		return nil, err
	}

	id := assistant.HeadIDPrefix + uuid.New().String()
	configs, cleanup := a.headMCPConfig(id)

	rt, err := a.mgr.StartPersonaRuntime(ctx, session.PersonaRuntimeParams{
		ID:              id,
		Preamble:        p.Preamble,
		Model:           p.Model,
		Effort:          p.Effort,
		WorkDir:         workDir,
		MCPConfigs:      configs,
		DisallowedTools: headDisallowedTools,
		OnText:          p.OnText,
		OnThought:       p.OnThought,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("start assistant head: %w", err)
	}
	return &assistantHead{runtime: rt, cleanup: cleanup}, nil
}

// headWorkDir is the directory the head's subprocess runs in.
//
// One stable directory rather than a temporary one per start, because nothing
// is supposed to be in it: it is where a CLI that has had its file tools taken
// away is pointed so that it is not pointed at the server's own cwd. It lives
// in the data directory, which is owner-only (paths.SecureDataDir, from the
// serve command).
func headWorkDir() (string, error) {
	dir := filepath.Join(paths.DataDir(), "assistant-head")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("assistant head work dir: %w", err)
	}
	return dir, nil
}

// headMCPConfig mints the head's bearer and writes it where only this user can
// read it, answering the configs to hand the CLI and the cleanup that undoes
// both.
//
// Every failure degrades to a head with no tools rather than to no head: an
// assistant that can talk and not act is a worse assistant, where one that
// cannot be spoken to is not an assistant. The fallback is deliberately NOT
// inline JSON — that is the one thing this must never do, and the stdio
// transport a session falls back to has nothing the head wants.
func (a *assistantHeads) headMCPConfig(id string) (configs []string, cleanup func()) {
	noop := func() {}
	if a.tokens == nil || a.mcpURL == "" {
		return nil, noop
	}

	token, err := a.tokens.Mint(id)
	if err != nil {
		slog.Warn("assistant head: no MCP token, so it runs without its verbs", "error", err)
		return nil, noop
	}

	path, err := writeHeadMCPConfig(id, session.AgentiqueMCPHTTPConfig(a.mcpURL, token))
	if err != nil {
		// The token exists and nothing can use it; revoke rather than leave a
		// live credential behind a file that was never written.
		a.tokens.Revoke(id)
		slog.Warn("assistant head: no MCP config file, so it runs without its verbs", "error", err)
		return nil, noop
	}

	return []string{path}, func() {
		a.tokens.Revoke(id)
		_ = os.Remove(path)
	}
}

// writeHeadMCPConfig persists the head's MCP config as an owner-only file and
// returns the path to hand the CLI. Only the path ever reaches argv.
//
// It lives beside the per-session config files, inside the data directory,
// which is itself owner-only (paths.SecureDataDir, called from the serve
// command). It gets its own writer rather than the session one because that
// one validates that its id is a UUID and the head's deliberately is not — the
// id becomes a filename, and a check of what the id IS is what keeps a caller
// from writing outside the directory. So this validates the same thing in its
// own vocabulary: the prefix, and a UUID behind it.
func writeHeadMCPConfig(id, cfg string) (string, error) {
	suffix, ok := strings.CutPrefix(id, assistant.HeadIDPrefix)
	if !ok || uuid.Validate(suffix) != nil {
		return "", fmt.Errorf("assistant head mcp config: %q is not a head id", id)
	}
	dir := filepath.Join(paths.DataDir(), "mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("assistant head mcp config: create dir: %w", err)
	}
	path := filepath.Join(dir, id+".json")
	// O_TRUNC rather than O_EXCL: the mode only applies on creation, so
	// SecureFile is what covers a file left behind by an earlier process that
	// died before its cleanup ran.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("assistant head mcp config: open %s: %w", path, err)
	}
	if _, err := f.WriteString(cfg); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("assistant head mcp config: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("assistant head mcp config: close %s: %w", path, err)
	}
	if err := paths.SecureFile(path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("assistant head mcp config: restrict %s: %w", path, err)
	}
	return path, nil
}

// SweepHeadCredentials removes the head MCP config files an earlier process
// left behind, answering how many went.
//
// A head's id is a fresh uuid per start, so a server killed mid-turn leaves a
// file under a name nothing will ever reuse. The token in it is already dead —
// the store is in memory and does not survive a restart — so this is litter
// rather than a live credential, which is why it is a sweep at boot and not a
// guard on the write. It is called from the serve command's production block:
// CLAUDE.md's rule is that a startup sweep never runs from a constructor a test
// might call.
func SweepHeadCredentials() (int, error) {
	pattern := filepath.Join(paths.DataDir(), "mcp", assistant.HeadIDPrefix+"*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("sweep assistant head credentials: %w", err)
	}
	var removed int
	var errs []error
	for _, path := range matches {
		if err := os.Remove(path); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	if len(errs) > 0 {
		return removed, fmt.Errorf("sweep assistant head credentials: %w", errors.Join(errs...))
	}
	return removed, nil
}

// assistantHead is one live head, plus the credential it holds.
//
// The wrapper exists for Close. Both halves of the credential are this file's:
// the token lives in the assistant's own store, which the session manager does
// not know about and therefore cannot revoke from, and the 0600 file holding it
// is written here. Neither may outlive the subprocess.
type assistantHead struct {
	runtime assistant.HeadRuntime
	cleanup func()
}

func (h *assistantHead) Query(ctx context.Context, prompt string) (string, error) {
	return h.runtime.Query(ctx, prompt)
}

func (h *assistantHead) Close() error {
	err := h.runtime.Close()
	if h.cleanup != nil {
		h.cleanup()
	}
	if err != nil {
		return fmt.Errorf("stop assistant head: %w", err)
	}
	return nil
}

// assistantStateObserver turns the session.state push into the two facts the
// journal records: a branch went in, and the operator filed a session away.
//
// It is three lines of mapping at the wiring site on purpose. The payload is a
// session.GitSnapshot and internal/assistant does not import the session
// pipeline, so this is where the snapshot's vocabulary meets the assistant's —
// and where the three-valued reading of an absent marker is already understood:
// an absent archivedAt means NOT archived, never "unchanged".
type assistantStateObserver struct{ svc *assistant.Service }

// OnEvent implements eventbus.Subscriber.
//
// The bus delivers synchronously, on whichever goroutine published — a git
// operation, an approval, a state refresh — so the work goes to a goroutine of
// its own. It is cheap in the common case (a set lookup that says "already
// journaled") and the common case is also every other push, filtered out here
// before anything is spawned.
func (o *assistantStateObserver) OnEvent(e eventbus.Event) {
	if e.Type != assistantStatePush {
		return
	}
	snapshot, ok := e.Payload.(session.GitSnapshot)
	if !ok {
		return
	}
	if !snapshot.WorktreeMerged && snapshot.ArchivedAt == "" {
		return
	}

	state := assistant.SessionState{
		SessionID:      snapshot.SessionID,
		WorktreeMerged: snapshot.WorktreeMerged,
		ArchivedAt:     snapshot.ArchivedAt,
		Version:        snapshot.Version,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), turnFactsBudget)
		defer cancel()
		o.svc.ObserveSessionState(ctx, state)
	}()
}

// assistantStatePush is the push type the observer reads. Spelled here rather
// than imported because the session package publishes it as a literal too.
const assistantStatePush = "session.state"
