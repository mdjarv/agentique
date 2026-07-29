package providers

import (
	"context"

	codexcli "github.com/allbin/codexcli-go"
)

// fallbackCacheHint is shown when codex's models_cache.json is missing —
// surfaces *why* the list is bare so the user knows to launch codex once.
const fallbackCacheHint = "Run codex once locally to populate the model registry."

func (c *Catalog) codexModels(ctx context.Context, resolved map[string]string) ProviderModels {
	if override, ok := c.applyOverride("codex"); ok {
		return override
	}

	entries, err := codexcli.ListModels(ctx)
	if err != nil {
		// Both the "cache missing" sentinel and any other read failure land on
		// the same minimal list; the hint tells the user how to populate it.
		return ProviderModels{
			Provider: "codex",
			Source:   "fallback",
			Models: []ModelInfo{
				{Slug: "gpt-5", DisplayName: "GPT-5", Description: fallbackCacheHint},
			},
		}
	}

	// Preserve codex's on-disk order — the cache is already priority-sorted
	// (most prominent first). Re-sorting here would invert that.
	out := make([]ModelInfo, 0, len(entries))
	for _, m := range entries {
		if m.Visibility != "" && m.Visibility != codexcli.VisibilityList {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		out = append(out, ModelInfo{
			Slug:        m.Slug,
			DisplayName: name,
			Description: m.Description,
			ResolvedID:  resolved[m.Slug],
		})
	}
	return ProviderModels{Provider: "codex", Source: "cache", Models: out}
}
