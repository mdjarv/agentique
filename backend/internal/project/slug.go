// Slug derivation for project names.
//
// A slug is ASCII by construction: it lands in URLs, in the CLI's `--project`
// flag and in the voice switchboard's spoken names, so the safe set is
// `[a-z0-9-]`. The naive way to get there is to replace everything else with a
// separator, but that treats a letter the same as punctuation: "Träffbild"
// becomes "tr-ffbild", which reads as two words and is not what anyone would
// type. So a letter is *transliterated* to its ASCII base first, and only what
// survives that is treated as a separator.
//
// A script with no ASCII base at all (Cyrillic, CJK, emoji) still strips away,
// and a name made entirely of those falls back to "project" plus a uniquifying
// suffix. The name itself is untouched — it is free Unicode text, and it is
// what the sidebar and the worktree directory use.
package project

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)
var validSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// asciiFold strips the combining marks NFD decomposition exposes, so "ä"
// (a + U+0308) reduces to "a". Built once: the transformer is stateful, so
// each use takes a Reset via transform.String.
var asciiFold = transform.Chain(
	norm.NFD,
	runes.Remove(runes.In(unicode.Mn)),
	norm.NFC,
)

// nonDecomposing covers the letters NFD leaves alone because they are not an
// ASCII letter plus a mark — they are their own letter. Decomposition cannot
// help there, so the conventional Latin spelling is spelled out. Keys are
// lowercase; Slugify lowercases before substituting.
var nonDecomposing = strings.NewReplacer(
	"ß", "ss",
	"ø", "o",
	"æ", "ae",
	"œ", "oe",
	"đ", "d",
	"ð", "d",
	"þ", "th",
	"ł", "l",
	"ħ", "h",
	"ı", "i",
	"ŋ", "ng",
	"ſ", "s",
	"·", "-",
	"&", "-and-",
)

// Slugify converts a name to a URL-safe lowercase ASCII slug, transliterating
// accented letters rather than discarding them ("Träffbild" -> "traffbild").
// Returns "project" when nothing survives.
func Slugify(name string) string {
	s := strings.ToLower(name)
	s = nonDecomposing.Replace(s)
	if folded, _, err := transform.String(asciiFold, s); err == nil {
		s = folded
	}
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "project"
	}
	return s
}
