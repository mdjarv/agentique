package memory

import (
	"strings"
	"unicode"
)

// stopwords are dropped before scoring so common glue words don't dominate
// keyword overlap. Kept deliberately small — domain terms must survive.
var stopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "but": {}, "if": {}, "then": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"to": {}, "of": {}, "in": {}, "on": {}, "at": {}, "for": {}, "with": {}, "by": {},
	"from": {}, "as": {}, "it": {}, "its": {}, "this": {}, "that": {}, "these": {},
	"those": {}, "i": {}, "me": {}, "my": {}, "we": {}, "our": {}, "you": {}, "your": {},
	"he": {}, "she": {}, "they": {}, "them": {}, "do": {}, "does": {}, "did": {},
	"have": {}, "has": {}, "had": {}, "will": {}, "would": {}, "can": {}, "could": {},
	"should": {}, "about": {}, "into": {}, "over": {}, "not": {}, "no": {}, "yes": {},
}

// tokenize lowercases, splits on non-alphanumeric runes, and drops stopwords and
// single-character tokens.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if _, ok := stopwords[f]; ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

// tokenSet returns the unique tokens of s.
func tokenSet(s string) map[string]struct{} {
	toks := tokenize(s)
	set := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		set[t] = struct{}{}
	}
	return set
}
