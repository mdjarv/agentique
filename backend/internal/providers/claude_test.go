package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestModelDisplayName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"claude-opus-5", "Opus 5"},
		{"claude-opus-4-8", "Opus 4.8"},
		{"claude-sonnet-4-6", "Sonnet 4.6"},
		{"claude-haiku-4-5-20251001", "Haiku 4.5"},
		{"claude-3-5-sonnet-20241022", "Sonnet 3.5"},
		{"claude-3-opus-20240229", "Opus 3"},
		{"claude-opus-5[1m]", "Opus 5"},
		// The family token is not an allowlist, so an unreleased family renders
		// without a code change — the whole point of deriving the label.
		{"claude-fable-5", "Fable 5"},
		{"claude-quartz-6-2", "Quartz 6.2"},
		{"opus", "Opus"},
		{"  claude-opus-5  ", "Opus 5"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ModelDisplayName(tt.in); got != tt.want {
			t.Errorf("ModelDisplayName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// noExtras points the CLI-options layer at a path that does not exist, so tests
// exercise the base + learned layers in isolation.
func noExtras(t *testing.T) Option {
	t.Helper()
	return WithCLIOptionsPath(filepath.Join(t.TempDir(), "absent.json"))
}

func claudeList(t *testing.T, c *Catalog) ProviderModels {
	t.Helper()
	for _, p := range c.ListModels(context.Background()).Providers {
		if p.Provider == "claude" {
			return p
		}
	}
	t.Fatal("no claude provider in catalog")
	return ProviderModels{}
}

func find(t *testing.T, pm ProviderModels, slug string) ModelInfo {
	t.Helper()
	for _, m := range pm.Models {
		if m.Slug == slug {
			return m
		}
	}
	t.Fatalf("slug %q not in catalog %+v", slug, pm.Models)
	return ModelInfo{}
}

func TestClaudeModelsWithoutResolutions(t *testing.T) {
	pm := claudeList(t, New(noExtras(t)))

	if pm.Source != "static" {
		t.Errorf("Source = %q, want static", pm.Source)
	}
	if got := find(t, pm, "opus[1m]").DisplayName; got != "Opus" {
		t.Errorf("opus[1m] label = %q, want Opus", got)
	}
	for _, m := range pm.Models {
		if m.Slug == "opus" || m.Slug == "sonnet" {
			t.Errorf("catalog includes non-1M duplicate %q", m.Slug)
		}
	}
}

func TestClaudeModelsKeepStableLabelsWithResolutions(t *testing.T) {
	resolver := ResolverFunc(func(context.Context) ([]Resolution, error) {
		return []Resolution{
			{Provider: "claude", Slug: "opus[1m]", ResolvedID: "claude-opus-5[1m]"},
			{Provider: "codex", Slug: "gpt-5", ResolvedID: "gpt-5"},
		}, nil
	})
	pm := claudeList(t, New(WithResolver(resolver), noExtras(t)))

	if pm.Source != "learned" {
		t.Errorf("Source = %q, want learned", pm.Source)
	}
	opus := find(t, pm, "opus[1m]")
	if opus.DisplayName != "Opus" {
		t.Errorf("opus label = %q, want Opus", opus.DisplayName)
	}
	if opus.ResolvedID != "claude-opus-5[1m]" {
		t.Errorf("opus resolvedId = %q, want claude-opus-5[1m]", opus.ResolvedID)
	}
	if got := find(t, pm, "sonnet[1m]").DisplayName; got != "Sonnet" {
		t.Errorf("sonnet label = %q, want Sonnet", got)
	}
}

func TestClaudeModelsResolverFailureDegrades(t *testing.T) {
	resolver := ResolverFunc(func(context.Context) ([]Resolution, error) {
		return nil, errors.New("db down")
	})
	pm := claudeList(t, New(WithResolver(resolver), noExtras(t)))

	if len(pm.Models) != len(claudeAliases) {
		t.Fatalf("got %d models, want %d — a resolver failure must not empty the catalog", len(pm.Models), len(claudeAliases))
	}
	if pm.Source != "static" {
		t.Errorf("Source = %q, want static", pm.Source)
	}
}

func TestClaudeModelsIncludesCLIAdvertisedExtras(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	body := `{"numStartups":3,"additionalModelOptionsCache":[
		{"value":"claude-fable-5[1m]","label":"Fable","description":"Most capable"},
		{"value":"claude-opus-5[1m]","label":"Opus","description":"dup of the opus alias"},
		{"value":"claude-quartz-6-2[1m]","label":"Quartz","description":"Experimental"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := ResolverFunc(func(context.Context) ([]Resolution, error) {
		return []Resolution{{Provider: "claude", Slug: "opus[1m]", ResolvedID: "claude-opus-5[1m]"}}, nil
	})
	pm := claudeList(t, New(WithResolver(resolver), WithCLIOptionsPath(path)))

	extra := find(t, pm, "claude-quartz-6-2[1m]")
	if extra.DisplayName != "Quartz" {
		t.Errorf("extra label = %q, want Quartz", extra.DisplayName)
	}
	if extra.Description != "Experimental" {
		t.Errorf("extra description = %q, want Experimental", extra.Description)
	}
	// Built-in families stay single choices even when the CLI advertises a
	// concrete or context-specific spelling for them.
	for _, m := range pm.Models {
		if m.Slug == "claude-opus-5[1m]" || m.Slug == "claude-fable-5[1m]" {
			t.Errorf("catalog lists %q separately from its alias", m.Slug)
		}
	}
}

func TestClaudeModelsMalformedCLIConfigIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	pm := claudeList(t, New(WithCLIOptionsPath(path)))
	if len(pm.Models) != len(claudeAliases) {
		t.Fatalf("got %d models, want %d", len(pm.Models), len(claudeAliases))
	}
}

func TestConfigOverrideReplacesCatalog(t *testing.T) {
	resolver := ResolverFunc(func(context.Context) ([]Resolution, error) {
		return []Resolution{{Provider: "claude", Slug: "opus", ResolvedID: "claude-opus-5"}}, nil
	})
	c := New(
		WithResolver(resolver),
		noExtras(t),
		WithOverrides(map[string][]Override{"claude": {
			{Slug: "opus", DisplayName: "Opus (pinned)"},
			{Slug: "", DisplayName: "dropped — no slug"},
		}}),
	)
	pm := claudeList(t, c)

	if pm.Source != "config" {
		t.Errorf("Source = %q, want config", pm.Source)
	}
	if len(pm.Models) != 1 {
		t.Fatalf("got %d models, want 1 (override replaces, not merges): %+v", len(pm.Models), pm.Models)
	}
	if pm.Models[0].DisplayName != "Opus (pinned)" {
		t.Errorf("label = %q, want Opus (pinned)", pm.Models[0].DisplayName)
	}
}

func TestOverrideWithoutDisplayFallsBackToSlug(t *testing.T) {
	c := New(noExtras(t), WithOverrides(map[string][]Override{"claude": {{Slug: "opus"}}}))
	if got := claudeList(t, c).Models[0].DisplayName; got != "opus" {
		t.Errorf("label = %q, want opus", got)
	}
}
