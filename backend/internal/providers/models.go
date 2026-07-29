// Package providers exposes provider-agnostic catalog data (model lists) to the
// wire layer.
//
// The catalog is assembled at request time from layers so that a new upstream
// model release does not require a new agentique release:
//
//  1. base    — the provider's stable alias set (claude) or its CLI's on-disk
//     model cache (codex).
//  2. learned — alias -> concrete ID mappings observed from provider init
//     events, which turn "opus" into "Opus 5" the moment the CLI
//     starts resolving it that way.
//  3. cli     — extra models the provider CLI itself advertises on disk.
//  4. config  — an explicit [models] override in config.toml, which wins.
package providers

import (
	"context"
	"log/slog"
)

// ModelInfo is the wire shape exposed to the frontend.
type ModelInfo struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// ResolvedID is the concrete upstream model ID this slug was last observed
	// to resolve to (e.g. "claude-opus-5" for "opus"). Empty until seen.
	ResolvedID string `json:"resolvedId,omitempty"`
}

// ProviderModels is the wire result of ListModels — one provider's catalog.
type ProviderModels struct {
	Provider string      `json:"provider"`
	Models   []ModelInfo `json:"models"`
	// Source names the strongest layer that shaped this list: "config" (an
	// explicit override), "learned" (labels derived from observed resolutions
	// or the provider CLI's on-disk options), "cache" (read wholesale from the
	// CLI's cache), "static" (base aliases only), or "fallback" (cache
	// unavailable). The frontend uses it to show staleness hints.
	Source string `json:"source"`
}

// ListModelsResult is the wire shape for the providers.models request.
type ListModelsResult struct {
	Providers []ProviderModels `json:"providers"`
}

// Resolution is one observed alias -> concrete model ID mapping.
type Resolution struct {
	Provider   string
	Slug       string
	ResolvedID string
}

// Resolver supplies observed model resolutions. Implementations are expected to
// be cheap (the table holds one row per slug ever used).
type Resolver interface {
	Resolutions(ctx context.Context) ([]Resolution, error)
}

// ResolverFunc adapts a plain function to Resolver.
type ResolverFunc func(ctx context.Context) ([]Resolution, error)

// Resolutions implements Resolver.
func (f ResolverFunc) Resolutions(ctx context.Context) ([]Resolution, error) { return f(ctx) }

// Override is a config-supplied model entry. A non-empty override list for a
// provider replaces that provider's generated list entirely.
type Override struct {
	Slug        string
	DisplayName string
	Description string
}

// Catalog assembles the per-provider model lists. Safe for concurrent use: it
// holds only read-only configuration and queries its dependencies per call.
type Catalog struct {
	resolver  Resolver
	overrides map[string][]Override
	// cliOptionsPath points at the claude CLI's config JSON. Empty means
	// "discover the default location"; tests set it explicitly.
	cliOptionsPath string
}

// Option configures a Catalog.
type Option func(*Catalog)

// WithResolver supplies observed alias -> concrete ID mappings.
func WithResolver(r Resolver) Option {
	return func(c *Catalog) { c.resolver = r }
}

// WithOverrides supplies config.toml [models] entries, keyed by provider.
func WithOverrides(o map[string][]Override) Option {
	return func(c *Catalog) { c.overrides = o }
}

// WithCLIOptionsPath points the claude layer at a specific config JSON file
// instead of the default location.
func WithCLIOptionsPath(path string) Option {
	return func(c *Catalog) { c.cliOptionsPath = path }
}

// New builds a Catalog.
func New(opts ...Option) *Catalog {
	c := &Catalog{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ListModels returns the catalog for every supported provider. Pure read; safe
// to call on any goroutine.
func (c *Catalog) ListModels(ctx context.Context) ListModelsResult {
	resolutions := c.resolutions(ctx)
	return ListModelsResult{
		Providers: []ProviderModels{
			c.claudeModels(resolutions["claude"]),
			c.codexModels(ctx, resolutions["codex"]),
		},
	}
}

// resolutions groups observed mappings by provider. A resolver failure degrades
// to no resolutions rather than failing the request — a stale label beats no
// catalog at all.
func (c *Catalog) resolutions(ctx context.Context) map[string]map[string]string {
	out := map[string]map[string]string{}
	if c.resolver == nil {
		return out
	}
	rows, err := c.resolver.Resolutions(ctx)
	if err != nil {
		slog.Warn("model catalog: resolutions unavailable, falling back to base labels", "error", err)
		return out
	}
	for _, r := range rows {
		if r.Provider == "" || r.Slug == "" || r.ResolvedID == "" {
			continue
		}
		if out[r.Provider] == nil {
			out[r.Provider] = map[string]string{}
		}
		out[r.Provider][r.Slug] = r.ResolvedID
	}
	return out
}

// applyOverride returns the config-supplied list for a provider, if any.
func (c *Catalog) applyOverride(provider string) (ProviderModels, bool) {
	entries := c.overrides[provider]
	if len(entries) == 0 {
		return ProviderModels{}, false
	}
	models := make([]ModelInfo, 0, len(entries))
	for _, e := range entries {
		if e.Slug == "" {
			continue
		}
		name := e.DisplayName
		if name == "" {
			name = e.Slug
		}
		models = append(models, ModelInfo{Slug: e.Slug, DisplayName: name, Description: e.Description})
	}
	if len(models) == 0 {
		return ProviderModels{}, false
	}
	return ProviderModels{Provider: provider, Source: "config", Models: models}, true
}
