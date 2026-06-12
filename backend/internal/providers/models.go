// Package providers exposes provider-agnostic catalog data (model lists)
// to the wire layer. Claude models are static aliases owned by claudecli-go;
// codex models are sourced from the codex CLI's on-disk models cache.
package providers

import (
	"context"
	"errors"

	codexcli "github.com/allbin/codexcli-go"
)

// ModelInfo is the wire shape exposed to the frontend.
type ModelInfo struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
}

// fallbackCacheHint is shown when codex's models_cache.json is missing —
// surfaces *why* the list is bare so the user knows to launch codex once.
const fallbackCacheHint = "Run codex once locally to populate the model registry."

// ProviderModels is the wire result of ListModels — one provider's catalog.
type ProviderModels struct {
	Provider string      `json:"provider"`
	Models   []ModelInfo `json:"models"`
	// Source is "static" (hard-coded), "cache" (read from codex's cache), or
	// "fallback" (cache unavailable, returning defaults). Useful for the
	// frontend to show staleness hints.
	Source string `json:"source"`
}

// ListModelsResult is the wire shape for the providers.models request.
type ListModelsResult struct {
	Providers []ProviderModels `json:"providers"`
}

// ListModels returns the catalog for every supported provider. Pure read; safe
// to call on any goroutine.
func ListModels(ctx context.Context) ListModelsResult {
	return ListModelsResult{
		Providers: []ProviderModels{
			claudeModels(),
			codexModels(ctx),
		},
	}
}

func claudeModels() ProviderModels {
	return ProviderModels{
		Provider: "claude",
		Source:   "static",
		Models: []ModelInfo{
			{Slug: "haiku", DisplayName: "Haiku 4.5"},
			{Slug: "sonnet", DisplayName: "Sonnet 4.6"},
			{Slug: "opus", DisplayName: "Opus 4.8"},
			{Slug: "fable", DisplayName: "Fable 5"},
			{Slug: "sonnet[1m]", DisplayName: "Sonnet 4.6 (1M)"},
			{Slug: "opus[1m]", DisplayName: "Opus 4.8 (1M)"},
		},
	}
}

func codexModels(ctx context.Context) ProviderModels {
	entries, err := codexcli.ListModels(ctx)
	if err != nil {
		if errors.Is(err, codexcli.ErrModelsCacheUnavailable) {
			return ProviderModels{
				Provider: "codex",
				Source:   "fallback",
				Models: []ModelInfo{
					{Slug: "gpt-5", DisplayName: "GPT-5", Description: fallbackCacheHint},
				},
			}
		}
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
		})
	}
	return ProviderModels{Provider: "codex", Source: "cache", Models: out}
}
