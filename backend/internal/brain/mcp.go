package brain

import (
	"context"
	"fmt"
	"strings"

	"github.com/allbin/agentkit/eventbus"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// ScopeResolver maps a session ID to the memory scope its memories belong to.
// Implemented in the server wiring using the session's project.
type ScopeResolver func(ctx context.Context, sessionID string) memory.Scope

// MCPAdapter exposes the brain to agents through the string-in/string-out
// contract used by the MCP memory tools. It resolves the calling session's scope
// so an agent only ever reads/writes memories for its own project (plus global).
type MCPAdapter struct {
	svc     *Service
	resolve ScopeResolver
	k       int
	bus     eventbus.Broadcaster // optional; broadcasts the flare when an agent adds a fact
}

// NewMCPAdapter returns an adapter over svc using resolve to map sessions to scopes.
func NewMCPAdapter(svc *Service, resolve ScopeResolver) *MCPAdapter {
	return &MCPAdapter{svc: svc, resolve: resolve, k: memory.DefaultRecallK}
}

// SetBus wires the broadcaster so an agent-saved memory flares the nav button.
func (a *MCPAdapter) SetBus(bus eventbus.Broadcaster) { a.bus = bus }

// MemoryAdd stores a fact for the calling session's scope.
func (a *MCPAdapter) MemoryAdd(ctx context.Context, sessionID, text, category string) (string, error) {
	scope := a.resolve(ctx, sessionID)
	r, err := a.svc.Add(ctx, scope, text, memory.Category(strings.TrimSpace(category)), memory.SourceAgent)
	if err != nil {
		return "", err
	}
	if a.bus != nil {
		a.bus.Broadcast(EventBrainUpdated, map[string]string{})
	}
	return fmt.Sprintf("Remembered [%s]: %s", r.Category, r.Text), nil
}

// MemoryFlag records that a recalled fact was found contradicted (RFC-LD D2): it
// weakens the fact and queues it for the user to confirm/correct/delete — never
// deletes it outright. The agent passes the id shown in MemorySearch output. Scoped:
// an agent may only flag facts in its own project or the global scope.
func (a *MCPAdapter) MemoryFlag(ctx context.Context, sessionID, id, reason string) (string, error) {
	if _, err := a.scopedGet(ctx, sessionID, id); err != nil {
		return "", err
	}
	flagged, err := a.svc.Flag(ctx, id, reason)
	if err != nil {
		return "", err
	}
	if a.bus != nil {
		a.bus.Broadcast(EventBrainUpdated, map[string]string{})
	}
	return fmt.Sprintf("Flagged for review [%s]: %s", flagged.Category, flagged.Text), nil
}

// MemoryUsed records the POSITIVE outcome (RFC-LD D2 positive half, brain-outcome-signal.md):
// an agent confirms a recalled fact was used/correct this session. It strengthens the fact and
// raises its confidence toward the corroboration ceiling — earned trust that can graduate a
// preference into the operating contract. Scoped like MemoryFlag: an agent may only mark facts
// in its own project or the global scope.
func (a *MCPAdapter) MemoryUsed(ctx context.Context, sessionID, id string) (string, error) {
	if _, err := a.scopedGet(ctx, sessionID, id); err != nil {
		return "", err
	}
	helped, err := a.svc.MarkHelped(ctx, id)
	if err != nil {
		return "", err
	}
	if a.bus != nil {
		a.bus.Broadcast(EventBrainUpdated, map[string]string{})
	}
	return fmt.Sprintf("Noted as useful [%s]: %s", helped.Category, helped.Text), nil
}

// scopedGet fetches a memory by id, enforcing that it belongs to the calling session's
// project or the global scope — the shared authorization for the agent-facing outcome tools
// (MemoryFlag, MemoryUsed) so an agent can never touch another project's facts.
func (a *MCPAdapter) scopedGet(ctx context.Context, sessionID, id string) (memory.Record, error) {
	scope := a.resolve(ctx, sessionID)
	r, err := a.svc.Get(ctx, id)
	if err != nil {
		return memory.Record{}, fmt.Errorf("no memory with id %q", id)
	}
	for _, s := range recallScopes(scope) {
		if r.Scope == s {
			return r, nil
		}
	}
	return memory.Record{}, fmt.Errorf("memory %q is not in your project or global scope", id)
}

// MemorySearch returns pinned plus query-relevant facts for the session's scope.
func (a *MCPAdapter) MemorySearch(ctx context.Context, sessionID, query string) (string, error) {
	scope := a.resolve(ctx, sessionID)
	res, err := a.svc.Recall(ctx, recallScopes(scope), query, a.k)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(res.Pinned)+len(res.Recalled))
	for _, r := range res.Pinned {
		ids = append(ids, r.ID)
	}
	for _, r := range res.Recalled {
		ids = append(ids, r.ID)
	}
	if len(ids) > 0 {
		_ = a.svc.MarkUsed(ctx, ids...)
	}
	return formatRecall(res), nil
}

func formatRecall(res memory.Result) string {
	if len(res.Pinned) == 0 && len(res.Recalled) == 0 {
		return "No relevant memories."
	}
	var b strings.Builder
	if len(res.Pinned) > 0 {
		b.WriteString("Pinned facts:\n")
		for _, r := range res.Pinned {
			writeRecallLine(&b, r)
		}
	}
	if len(res.Recalled) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Relevant facts:\n")
		for _, r := range res.Recalled {
			writeRecallLine(&b, r)
		}
	}
	// The id lets the agent feed the outcome loop (RFC-LD D2): MemoryUsed when a fact
	// helped, MemoryFlag when it's wrong. Both take the id shown above.
	b.WriteString("\n(If a fact helped you, call MemoryUsed with its id; if any fact is wrong or outdated, call MemoryFlag with its id.)")
	return strings.TrimRight(b.String(), "\n")
}

// writeRecallLine renders one recalled fact, including its id so the agent can
// reference it in a later MemoryFlag call.
func writeRecallLine(b *strings.Builder, r memory.Record) {
	fmt.Fprintf(b, "- [%s] %s (id: %s)\n", r.Category, r.Text, r.ID)
}
