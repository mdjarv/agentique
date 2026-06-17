// Package mcphttp wires agentique's tools (channel SendMessage, dev URL
// management, session renaming) onto agentkit's reusable MCP-over-HTTP
// handler. The transport, schema, and dispatch live in agentkit/mcphttp;
// this package only contributes the agentique-specific tool implementations
// and tool-name constants that the permission interceptor keys on.
package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/allbin/agentkit/devurls"
	akmcp "github.com/allbin/agentkit/mcphttp"
)

// Tool name constants. The full names (mcp__<server>__<tool>) drive the
// permission interceptor in the session package, so they live here as the
// product-internal source of truth even though agentkit's mcphttp package
// is what actually serves them.
const (
	ServerName         = "agentique"
	ToolSendMessage    = "SendMessage"
	ToolAcquireDev     = "AcquireDevUrl"
	ToolReleaseDev     = "ReleaseDevUrl"
	ToolListDevURLs    = "ListDevUrls"
	ToolKillDevPort    = "KillDevUrlPort"
	ToolSetSessionName = "SetSessionName"
	ToolMemoryAdd      = "MemoryAdd"
	ToolMemorySearch   = "MemorySearch"
	ToolMemoryFlag     = "MemoryFlag"

	SendMessageToolFullName    = "mcp__" + ServerName + "__" + ToolSendMessage
	AcquireDevURLToolFullName  = "mcp__" + ServerName + "__" + ToolAcquireDev
	ReleaseDevURLToolFullName  = "mcp__" + ServerName + "__" + ToolReleaseDev
	ListDevURLsToolFullName    = "mcp__" + ServerName + "__" + ToolListDevURLs
	SetSessionNameToolFullName = "mcp__" + ServerName + "__" + ToolSetSessionName
	// KillDevPortToolFullName is NOT auto-approved: killing a process is
	// destructive, so the user must confirm each invocation.
	KillDevPortToolFullName = "mcp__" + ServerName + "__" + ToolKillDevPort
)

// serverVersion is reported in the initialize response. Bumped when the
// tool surface changes in a user-visible way.
const serverVersion = "1.0.0"

// maxSessionName clamps the SetSessionName argument so an over-eager agent
// can't write a paragraph into the sidebar.
const maxSessionName = 80

// TokenStore is the per-session bearer-token store backing the /mcp endpoint.
// Aliased from agentkit so call sites in this repo don't need to import the
// upstream package directly.
type TokenStore = akmcp.TokenStore

// NewTokenStore returns an empty TokenStore.
func NewTokenStore() *TokenStore { return akmcp.NewTokenStore() }

// SessionRenamer renames an existing session. Implemented by session.Service.
type SessionRenamer interface {
	RenameSession(ctx context.Context, sessionID, name string) error
}

// MemoryStore is the agent-facing contract for the brain memory tools. It is
// scoped per session by the implementation (an agent only sees its own project's
// memories plus global). Implemented by brain.MCPAdapter. May be nil — the
// memory tools are then not registered.
type MemoryStore interface {
	MemoryAdd(ctx context.Context, sessionID, text, category string) (string, error)
	MemorySearch(ctx context.Context, sessionID, query string) (string, error)
	MemoryFlag(ctx context.Context, sessionID, id, reason string) (string, error)
}

// NewHandler returns the configured /mcp http.Handler. renamer may be nil in
// tests that don't exercise SetSessionName — calls to that tool will then
// return an error result. mem may be nil to omit the brain memory tools.
func NewHandler(tokens *TokenStore, dev *devurls.Store, renamer SessionRenamer, mem MemoryStore) http.Handler {
	h := akmcp.New(ServerName, tokens, akmcp.WithServerVersion(serverVersion))

	register(h, akmcp.Tool{
		Name:        ToolSendMessage,
		Description: "Send a message to a teammate in this channel.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"to": akmcp.StringProp{
					Description: "Recipient: teammate name, or \"@spawn\" to create workers, or \"@dissolve\" to close the channel.",
				},
				"message": akmcp.StringProp{
					Description: "Message content. For @spawn, a JSON string with channelName and workers array.",
				},
				"type": akmcp.StringProp{
					Enum:        []string{"plan", "progress", "done", "message"},
					Description: "Message type for status signaling.",
				},
			},
			Required: []string{"to", "message"},
		},
		Handler: func(_ context.Context, _ string, _ json.RawMessage) akmcp.Result {
			// Should never reach here — Claude's permission gate intercepts
			// SendMessage before it executes. Return a benign success to match
			// mcp-channel behavior.
			return akmcp.TextResult("Message delivered.")
		},
	})

	register(h, akmcp.Tool{
		Name:        ToolAcquireDev,
		Description: "Lease a publicly-routable HTTPS URL that points at a local TCP port on this machine. Bind any HTTP service to the returned port and it becomes reachable at the returned URL (TLS terminated by the reverse proxy — valid certificate, so HTTPS-only features like passkeys/WebAuthn, secure cookies, and service workers work). Returns {slot, url, publicHost, port}. Idempotent — re-calling returns the existing lease for this session.",
		Handler: func(ctx context.Context, sid string, _ json.RawMessage) akmcp.Result {
			return acquireDevImpl(ctx, dev, sid)
		},
	})

	register(h, akmcp.Tool{
		Name:        ToolReleaseDev,
		Description: "Release any dev URL slot leased by this session. Idempotent — no-op if nothing is held. Slots also auto-release when the session ends.",
		Handler: func(_ context.Context, sid string, _ json.RawMessage) akmcp.Result {
			return releaseDevImpl(dev, sid)
		},
	})

	register(h, akmcp.Tool{
		Name:        ToolListDevURLs,
		Description: "List all configured dev URL slots, their current holders, and whether each port is actually bound. Includes external-owner details (pid, cmdline, cwd) when a port is bound by a process not tracked by the lease store — useful for spotting orphans that need KillDevUrlPort.",
		Handler: func(ctx context.Context, _ string, _ json.RawMessage) akmcp.Result {
			return listDevURLsImpl(ctx, dev)
		},
	})

	type setNameArgs struct {
		Name string `json:"name"`
	}
	register(h, akmcp.Tool{
		Name:        ToolSetSessionName,
		Description: "Rename the current Agentique session. Use when the session's topic becomes clear or the user asks for a rename. Keep the name short (a few words, max 80 chars) and descriptive of what the session is about — it appears in the sidebar. The UI updates immediately.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"name": akmcp.StringProp{
					Description: "New session title. Short, human-readable, no trailing punctuation.",
				},
			},
			Required: []string{"name"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args setNameArgs) akmcp.Result {
			return setSessionNameImpl(ctx, renamer, sid, args.Name)
		}),
	})

	type killSlotArgs struct {
		Slot string `json:"slot"`
	}
	register(h, akmcp.Tool{
		Name:        ToolKillDevPort,
		Description: "Terminate the process currently listening on a dev URL slot's TCP port. Use when AcquireDevUrl skipped a slot or ListDevUrls reports an external/orphan owner. SIGTERM → 2s wait → SIGKILL. Destructive — requires user confirmation each call. After success, retry AcquireDevUrl.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"slot": akmcp.StringProp{
					Description: "Slot name (e.g. \"dev1\"). See ListDevUrls for configured slots.",
				},
			},
			Required: []string{"slot"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, _ string, args killSlotArgs) akmcp.Result {
			return killDevPortImpl(ctx, dev, args.Slot)
		}),
	})

	if mem != nil {
		registerMemoryTools(h, mem)
	}

	return h
}

func registerMemoryTools(h *akmcp.Handler, mem MemoryStore) {
	type addArgs struct {
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	register(h, akmcp.Tool{
		Name:        ToolMemoryAdd,
		Description: "Save a durable fact to your persistent memory ('brain') for this project. Use for things worth remembering across sessions: user preferences, project conventions, architectural decisions, gotchas. Keep each fact short and self-contained. Do NOT save transient task state or secrets.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"text": akmcp.StringProp{Description: "The fact to remember, phrased as a standalone statement."},
				"category": akmcp.StringProp{
					Enum:        []string{"fact", "identity", "preference", "contact", "project", "goal", "task"},
					Description: "Kind of fact. 'identity' facts are auto-pinned.",
				},
			},
			Required: []string{"text"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args addArgs) akmcp.Result {
			msg, err := mem.MemoryAdd(ctx, sid, args.Text, args.Category)
			if err != nil {
				return akmcp.ErrorResultf("memory add failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})

	type searchArgs struct {
		Query string `json:"query"`
	}
	register(h, akmcp.Tool{
		Name:        ToolMemorySearch,
		Description: "Search your persistent memory ('brain') for facts relevant to a query, plus always-included pinned facts. Call this at the start of a task to recall what you already know about this project and the user's preferences.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"query": akmcp.StringProp{Description: "What you want to recall (keywords or a short question)."},
			},
			Required: []string{"query"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args searchArgs) akmcp.Result {
			msg, err := mem.MemorySearch(ctx, sid, args.Query)
			if err != nil {
				return akmcp.ErrorResultf("memory search failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})

	type flagArgs struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	register(h, akmcp.Tool{
		Name:        ToolMemoryFlag,
		Description: "Flag a memory from your 'brain' as wrong or outdated when something you found this session contradicts it. Pass the fact's id (shown by MemorySearch) and a short reason. This does NOT delete it — it weakens the fact and queues it for the user to confirm, correct, or remove. Use it whenever a recalled fact turns out to be incorrect.",
		InputSchema: akmcp.ObjectProp{
			Properties: map[string]akmcp.Property{
				"id":     akmcp.StringProp{Description: "The id of the memory to flag (from MemorySearch output)."},
				"reason": akmcp.StringProp{Description: "Briefly, what contradicts this fact or why it's outdated."},
			},
			Required: []string{"id"},
		},
		Handler: akmcp.TypedHandler(func(ctx context.Context, sid string, args flagArgs) akmcp.Result {
			msg, err := mem.MemoryFlag(ctx, sid, args.ID, args.Reason)
			if err != nil {
				return akmcp.ErrorResultf("memory flag failed: %v", err)
			}
			return akmcp.TextResult(msg)
		}),
	})
}

// register panics on registration failures because they indicate programmer
// errors (duplicate names, malformed schemas) that must be caught at startup,
// not at request time.
func register(h *akmcp.Handler, t akmcp.Tool) {
	if err := h.Register(t); err != nil {
		panic(fmt.Sprintf("mcphttp: register %q: %v", t.Name, err))
	}
}

// --- tool implementations ---

func acquireDevImpl(ctx context.Context, dev *devurls.Store, sessionID string) akmcp.Result {
	if len(dev.Slots(ctx)) == 0 {
		return akmcp.ErrorResult("No dev URL slots are configured on this server. Ask the operator to add [[dev-urls]] entries to agentique config.")
	}
	res, err := dev.Acquire(ctx, sessionID)
	if err != nil {
		if errors.Is(err, devurls.ErrAllBusy) {
			return akmcp.ErrorResult("All dev URL slots are currently in use.\n" + summarizeSlotState(dev.Slots(ctx)) + "\n\n" +
				"Use KillDevUrlPort with a specific slot name to reclaim a port held by an external/orphan process (requires user confirmation).")
		}
		return akmcp.ErrorResultf("acquire failed: %v", err)
	}
	lease := res.Lease
	msg := fmt.Sprintf(
		"Acquired dev URL slot %q.\n"+
			"Public URL: %s (TLS-terminated by the reverse proxy)\n"+
			"Local port: %d\n"+
			"Public host: %s\n\n"+
			"Bind any HTTP service to 127.0.0.1:%d (or 0.0.0.0:%d) and it becomes reachable at the URL. Examples:\n"+
			"  - Vite dev server:  `just dev-frontend-remote %d %s` (Agentique) or `vite --port %d --host`\n"+
			"  - Go HTTP server:   pass `--addr :%d` or `http.ListenAndServe(\":%d\", ...)`\n"+
			"  - Any bind-to-port process works (static file servers, tunneled demos, etc.)\n\n"+
			"Release with ReleaseDevUrl when done (auto-released at session end).",
		lease.Slot, lease.URL, lease.Port, lease.PublicHost,
		lease.Port, lease.Port,
		lease.Port, lease.PublicHost, lease.Port,
		lease.Port, lease.Port,
	)
	if len(res.Skipped) > 0 {
		msg += "\n\nNote: skipped these slots because their ports are bound by external/orphan processes:\n" +
			formatConflicts(res.Skipped) +
			"\nConsider calling KillDevUrlPort to clean them up."
	}
	return akmcp.TextResult(msg)
}

func releaseDevImpl(dev *devurls.Store, sessionID string) akmcp.Result {
	freed := dev.Release(sessionID)
	if len(freed) == 0 {
		return akmcp.TextResult("No dev URL slot was held by this session.")
	}
	return akmcp.TextResult("Released slot(s): " + strings.Join(freed, ", "))
}

func listDevURLsImpl(ctx context.Context, dev *devurls.Store) akmcp.Result {
	infos := dev.Slots(ctx)
	if len(infos) == 0 {
		return akmcp.TextResult("No dev URL slots are configured.")
	}
	return akmcp.TextResult("Dev URL slots:\n" + summarizeSlotState(infos))
}

func killDevPortImpl(ctx context.Context, dev *devurls.Store, slotName string) akmcp.Result {
	if strings.TrimSpace(slotName) == "" {
		return akmcp.ErrorResult("KillDevUrlPort requires { slot: \"<slot name>\" }. Use ListDevUrls to see configured slots.")
	}
	slot, ok := dev.FindSlot(slotName)
	if !ok {
		return akmcp.ErrorResultf("unknown slot %q", slotName)
	}
	owner, err := devurls.FindPortOwner(ctx, slot.Port)
	if err != nil {
		return akmcp.ErrorResultf("lookup owner for port %d: %v", slot.Port, err)
	}
	if owner == nil {
		// Nothing bound. Also clear any stale lease so the slot is reusable.
		dev.ReleaseSlot(slot.Slot)
		return akmcp.TextResultf("Port %d is already free. Cleared any stale lease on slot %q.", slot.Port, slot.Slot)
	}

	proc, err := os.FindProcess(owner.PID)
	if err != nil {
		return akmcp.ErrorResultf("find process %d: %v", owner.PID, err)
	}
	_ = proc.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(owner.PID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	killed := false
	if pidAlive(owner.PID) {
		_ = proc.Signal(syscall.SIGKILL)
		time.Sleep(200 * time.Millisecond)
		killed = pidAlive(owner.PID) // true means KILL also failed
	}

	// Clear any lease tracking this slot — whoever held it is gone now.
	dev.ReleaseSlot(slot.Slot)

	if killed {
		return akmcp.ErrorResultf("Sent SIGTERM+SIGKILL to pid %d but it is still alive. Manual intervention needed.", owner.PID)
	}
	return akmcp.TextResultf("Killed pid %d (%s). Slot %q (port %d) is free — retry AcquireDevUrl.",
		owner.PID, owner.Describe(), slot.Slot, slot.Port)
}

func setSessionNameImpl(ctx context.Context, renamer SessionRenamer, sessionID, raw string) akmcp.Result {
	if renamer == nil {
		return akmcp.ErrorResult("SetSessionName is not available in this server.")
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return akmcp.ErrorResult("name is empty — provide a short human-readable title.")
	}
	if len(name) > maxSessionName {
		name = strings.TrimSpace(name[:maxSessionName])
	}
	if err := renamer.RenameSession(ctx, sessionID, name); err != nil {
		return akmcp.ErrorResultf("rename failed: %v", err)
	}
	return akmcp.TextResultf("Session renamed to %q.", name)
}

func pidAlive(pid int) bool {
	// On Linux, signal 0 probes existence without affecting the process.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func summarizeSlotState(infos []devurls.SlotInfo) string {
	sorted := make([]devurls.SlotInfo, len(infos))
	copy(sorted, infos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slot < sorted[j].Slot })
	lines := make([]string, 0, len(sorted))
	for _, i := range sorted {
		status := "(free)"
		switch {
		case i.HolderSessionID != "" && i.PortBusy:
			status = fmt.Sprintf("held by %s (port bound)", i.HolderSessionID)
		case i.HolderSessionID != "" && !i.PortBusy:
			status = fmt.Sprintf("leased by %s but port is NOT bound — stale lease", i.HolderSessionID)
		case i.HolderSessionID == "" && i.PortBusy:
			status = "external owner — " + i.ExternalOwner.Describe()
		}
		lines = append(lines, fmt.Sprintf("- %s → %s (port %d): %s", i.Slot, i.URL, i.Port, status))
	}
	return strings.Join(lines, "\n")
}

func formatConflicts(cs []devurls.SlotConflict) string {
	lines := make([]string, 0, len(cs))
	for _, c := range cs {
		lines = append(lines, fmt.Sprintf("- %s (port %d): %s", c.Slot, c.Port, c.Owner.Describe()))
	}
	return strings.Join(lines, "\n")
}
