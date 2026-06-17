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
			fmt.Fprintf(&b, "- [%s] %s\n", r.Category, r.Text)
		}
	}
	if len(res.Recalled) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Relevant facts:\n")
		for _, r := range res.Recalled {
			fmt.Fprintf(&b, "- [%s] %s\n", r.Category, r.Text)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
