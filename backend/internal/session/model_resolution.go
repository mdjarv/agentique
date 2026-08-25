package session

import (
	"context"
	"log/slog"

	"github.com/allbin/agentkit/sqliteops"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// persistResolvedModel records the concrete model a session actually ran on,
// as reported by the provider's init event.
//
// Two writes, deliberately different in kind: the session row is history ("what
// did this conversation run on"), while the (provider, slug) row lets the model
// catalog recognize concrete CLI options already covered by a stable alias.
//
// Both failures are logged and swallowed: a session must not die because a
// catalog hint could not be stored.
func persistResolvedModel(p sessionParams, resolvedID string) {
	if resolvedID == "" {
		return
	}

	if err := sqliteops.RetryWrite(func() error {
		return p.queries.UpdateSessionResolvedModel(context.Background(), store.UpdateSessionResolvedModelParams{
			ResolvedModel: resolvedID,
			ID:            p.id,
		})
	}); err != nil {
		slog.Error("persist resolved model failed", "session_id", p.id, "model_id", resolvedID, "error", err)
	}

	// Only alias → concrete pairs teach the catalog anything. A session started
	// on a pinned ID (slug == resolved) or with no slug at all has nothing new.
	if p.model == "" || p.model == resolvedID {
		return
	}
	if err := sqliteops.RetryWrite(func() error {
		return p.queries.UpsertModelResolution(context.Background(), store.UpsertModelResolutionParams{
			Provider:   normalizeProvider(p.provider),
			Slug:       p.model,
			ResolvedID: resolvedID,
		})
	}); err != nil {
		slog.Error("persist model resolution failed", "slug", p.model, "model_id", resolvedID, "error", err)
	}
}
