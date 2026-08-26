package providers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The catalog is the vocabulary. A spoken family name resolves through the same
// list the picker renders, so a new upstream family needs no release here.
func TestResolveFamilyAcceptsWhatAPersonSays(t *testing.T) {
	catalog := testCatalog(t)

	tests := []struct {
		spoken   string
		wantSlug string
	}{
		{"fable", "fable"},
		{"Fable", "fable"},
		{"  OPUS  ", "opus[1m]"},
		{"claude opus", "opus[1m]"},
		{"the opus model", "opus[1m]"},
		{"opus 5", "opus[1m]"},
		{"opus[1m]", "opus[1m]"},
		{"sonnet", "sonnet[1m]"},
		{"haiku", "haiku"},
	}

	for _, tt := range tests {
		t.Run(tt.spoken, func(t *testing.T) {
			got, ok := catalog.ResolveFamily(context.Background(), "claude", tt.spoken)
			if !ok {
				t.Fatalf("ResolveFamily(%q) found nothing", tt.spoken)
			}
			if got.Slug != tt.wantSlug {
				t.Errorf("ResolveFamily(%q) = %q, want %q", tt.spoken, got.Slug, tt.wantSlug)
			}
		})
	}
}

// Nothing is guessed. A word the catalog does not have resolves to nothing, so
// the caller has to ask rather than run something nobody named.
func TestResolveFamilyNeverGuesses(t *testing.T) {
	catalog := testCatalog(t)
	for _, spoken := range []string{"", "   ", "grok", "the fastest one", "claude"} {
		if got, ok := catalog.ResolveFamily(context.Background(), "claude", spoken); ok {
			t.Errorf("ResolveFamily(%q) guessed %q", spoken, got.Slug)
		}
	}
	if _, ok := catalog.ResolveFamily(context.Background(), "nosuchprovider", "opus"); ok {
		t.Error("an unknown provider resolved a family")
	}
}

// A refusal has to say what there IS, and it says it in labels — a slug read
// aloud is noise.
func TestFamilyNamesAreSpeakableAndDistinct(t *testing.T) {
	names := testCatalog(t).FamilyNames(context.Background(), "claude")
	if len(names) == 0 {
		t.Fatal("no families to offer")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("%q listed twice", name)
		}
		seen[name] = true
		if strings.ContainsAny(name, "[]-") {
			t.Errorf("%q is a slug, not something to say out loud", name)
		}
	}
	for _, want := range []string{"Opus", "Fable", "Sonnet", "Haiku"} {
		if !seen[want] {
			t.Errorf("families %v do not include %q", names, want)
		}
	}
}

// An override replaces the family vocabulary, because it replaces the picker's.
func TestFamilyNamesFollowConfigOverrides(t *testing.T) {
	catalog := New(WithOverrides(map[string][]Override{
		"claude": {{Slug: "house-model", DisplayName: "House"}},
	}))
	names := catalog.FamilyNames(context.Background(), "claude")
	if len(names) != 1 || names[0] != "House" {
		t.Fatalf("families = %v, want the override's label", names)
	}
	got, ok := catalog.ResolveFamily(context.Background(), "claude", "house")
	if !ok || got.Slug != "house-model" {
		t.Errorf("ResolveFamily = %v/%v, want the override's slug", got, ok)
	}
	if _, ok := catalog.ResolveFamily(context.Background(), "claude", "opus"); ok {
		t.Error("an override that replaced the list still answered for a family it does not have")
	}
}

// testCatalog points the CLI layer at a file that does not exist, so the test
// asserts the base aliases rather than whatever the developer's own
// ~/.claude.json happens to advertise.
func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	return New(WithCLIOptionsPath(filepath.Join(t.TempDir(), "absent.json")))
}
