package providers

import (
	"context"
	"strings"
	"unicode"
)

// Spoken model names.
//
// A picker label is a stable family name — "Opus", "Fable" — and that is also
// what a person says out loud. So resolving a spoken word to a model is a
// lookup in the catalog the picker already renders, never a table of ids kept
// somewhere else: a new upstream family reaches the voice assistant on the same
// day it reaches the picker, with no agentique release.
//
// Nothing here guesses. An unrecognised word resolves to nothing, and the
// caller is expected to say which families it does have rather than picking one.

// FamilyNames lists the distinct family labels a provider offers, in catalog
// order. It is what a refusal reads out, so it is labels ("Opus"), never slugs
// ("opus[1m]").
//
// Listing never fails: a catalog that cannot reach its layers still answers
// with the base aliases, and an unknown provider answers with nothing.
func (c *Catalog) FamilyNames(ctx context.Context, provider string) []string {
	var out []string
	seen := map[string]bool{}
	for _, model := range c.providerModels(ctx, provider) {
		label := familyLabel(model)
		if label == "" || seen[strings.ToLower(label)] {
			continue
		}
		seen[strings.ToLower(label)] = true
		out = append(out, label)
	}
	return out
}

// ResolveFamily maps a spoken family name to one of a provider's catalog
// entries. The second return is false for anything it does not recognise —
// there is no nearest match, because running the wrong model is worse than one
// spoken question.
//
// Matching is case-insensitive and tolerates the words that ride along with a
// name in speech ("claude opus", "the opus model", "opus 5"): the version is
// dropped because a family label carries none, which is the whole reason the
// picker's labels are stable.
func (c *Catalog) ResolveFamily(ctx context.Context, provider, spoken string) (ModelInfo, bool) {
	keys := spokenKeys(spoken)
	if len(keys) == 0 {
		return ModelInfo{}, false
	}

	models := c.providerModels(ctx, provider)
	// Whole phrase first, then the bare family word: "opus 5" must not match a
	// different family just because its first token is shorter.
	for _, key := range keys {
		for _, model := range models {
			if modelKeys(model)[key] {
				return model, true
			}
		}
	}
	return ModelInfo{}, false
}

// providerModels returns one provider's catalog list.
func (c *Catalog) providerModels(ctx context.Context, provider string) []ModelInfo {
	for _, p := range c.ListModels(ctx).Providers {
		if p.Provider == provider {
			return p.Models
		}
	}
	return nil
}

// familyLabel is what to call a catalog entry out loud: its display label,
// falling back to the family parsed out of its slug.
func familyLabel(model ModelInfo) string {
	if model.DisplayName != "" {
		return model.DisplayName
	}
	return ModelFamilyName(model.Slug)
}

// modelKeys is every spelling of one catalog entry a person might say.
func modelKeys(model ModelInfo) map[string]bool {
	keys := map[string]bool{}
	for _, candidate := range []string{
		model.DisplayName,
		model.Slug,
		ModelFamilyName(model.Slug),
		ModelFamilyName(model.DisplayName),
	} {
		if key := strings.Join(familyTokens(candidate), " "); key != "" {
			keys[key] = true
		}
	}
	return keys
}

// spokenKeys is what the caller might have meant, most specific first: the
// whole phrase, then its first word alone.
func spokenKeys(spoken string) []string {
	tokens := familyTokens(spoken)
	if len(tokens) == 0 {
		return nil
	}
	keys := []string{strings.Join(tokens, " ")}
	if len(tokens) > 1 {
		keys = append(keys, tokens[0])
	}
	return keys
}

// familyFiller is the words that ride along with a model name in speech and
// carry no identity of their own.
var familyFiller = map[string]bool{
	"claude": true, "model": true, "the": true, "a": true, "an": true,
	"use": true, "using": true, "please": true, "one": true, "with": true,
}

// familyTokens lowercases and drops punctuation, the bracketed context marker
// and conversational filler. "claude opus[1m]" and "the Opus model" both become
// ["opus"].
func familyTokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		// "1m" is a context window, not a family, and neither is a date stamp.
		if familyFiller[field] || field == "1m" || isAllDigits(field) {
			continue
		}
		out = append(out, field)
	}
	return out
}
