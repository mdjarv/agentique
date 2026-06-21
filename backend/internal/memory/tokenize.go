package memory

import (
	"strings"
	"unicode"
)

// stopwords are dropped before scoring so common glue words don't dominate keyword
// overlap. Domain terms must survive, so the list stays tight — but it MUST cover
// conversational/instructional filler ("like", "want", "please"…), not just grammatical
// glue. Reason: stored facts are terse and distilled, so a filler word is *rare across
// the corpus* and idf therefore scores it as a strong signal. A natural query like "I'd
// **like** you to investigate…" then spuriously matches any fact that happens to contain
// "like" (e.g. "…'like a Japanese garden'"). Filler words carry no retrieval intent, so
// they are dropped here rather than left for idf to over-reward.
var stopwords = map[string]struct{}{
	// Grammatical glue.
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "but": {}, "if": {}, "then": {},
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"to": {}, "of": {}, "in": {}, "on": {}, "at": {}, "for": {}, "with": {}, "by": {},
	"from": {}, "as": {}, "it": {}, "its": {}, "this": {}, "that": {}, "these": {},
	"those": {}, "i": {}, "me": {}, "my": {}, "we": {}, "our": {}, "you": {}, "your": {},
	"he": {}, "she": {}, "they": {}, "them": {}, "do": {}, "does": {}, "did": {},
	"have": {}, "has": {}, "had": {}, "will": {}, "would": {}, "can": {}, "could": {},
	"should": {}, "about": {}, "into": {}, "over": {}, "not": {}, "no": {}, "yes": {},
	// Question/relativizer words (non-domain). "where"/"while" omitted — they double as
	// SQL/loop constructs.
	"what": {}, "when": {}, "which": {}, "who": {}, "whom": {},
	"how": {}, "why": {}, "than": {}, "there": {}, "their": {}, "whether": {},
	// Conversational/instructional filler — the class that idf over-rewards in a terse
	// fact corpus. These never carry retrieval intent.
	"like": {}, "want": {}, "wants": {}, "wanted": {}, "need": {}, "needs": {},
	"needed": {}, "please": {}, "lets": {}, "help": {},
	"really": {}, "very": {}, "also": {}, "too": {}, "able": {}, "trying": {},
	"maybe": {}, "perhaps": {},
	// NB: "just" is deliberately NOT here — it's the `just` build tool / justfile, a
	// domain term in these projects. "let"/"try"/"where"/"while" are likewise omitted as
	// they double as code constructs. Filler only when unambiguous.
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

// TokenCount returns how many distinct significant tokens a string yields after
// stopword/length filtering — a cheap proxy for whether a message carries enough
// retrieval intent to recall against (vs. "ok" or "go for it").
func TokenCount(s string) int { return len(tokenSet(s)) }

// tokenSet returns the unique tokens of s.
func tokenSet(s string) map[string]struct{} {
	toks := tokenize(s)
	set := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		set[t] = struct{}{}
	}
	return set
}
