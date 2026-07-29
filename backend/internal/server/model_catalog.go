package server

import (
	"context"
	"fmt"

	"github.com/mdjarv/agentique/backend/internal/config"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// modelCatalog builds the model catalog served over the wire. It reads observed
// alias resolutions per request (the table holds one row per slug ever used, so
// there is nothing to cache) and layers config overrides on top.
func modelCatalog(queries *store.Queries, overrides map[string][]config.ModelOverride) *providers.Catalog {
	resolver := providers.ResolverFunc(func(ctx context.Context) ([]providers.Resolution, error) {
		rows, err := queries.ListModelResolutions(ctx)
		if err != nil {
			return nil, fmt.Errorf("list model resolutions: %w", err)
		}
		out := make([]providers.Resolution, 0, len(rows))
		for _, r := range rows {
			out = append(out, providers.Resolution{
				Provider:   r.Provider,
				Slug:       r.Slug,
				ResolvedID: r.ResolvedID,
			})
		}
		return out, nil
	})

	return providers.New(
		providers.WithResolver(resolver),
		providers.WithOverrides(providerOverrides(overrides)),
	)
}

func providerOverrides(in map[string][]config.ModelOverride) map[string][]providers.Override {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]providers.Override, len(in))
	for provider, entries := range in {
		converted := make([]providers.Override, 0, len(entries))
		for _, e := range entries {
			converted = append(converted, providers.Override{
				Slug:        e.Slug,
				DisplayName: e.Display,
				Description: e.Description,
			})
		}
		out[provider] = converted
	}
	return out
}
