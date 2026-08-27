package project

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Project", "my-project"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
		{"special!@#chars", "special-chars"},
		{"", "project"},
		{"---", "project"},
		{"hello world 123", "hello-world-123"},
		{"a", "a"},

		// A letter is transliterated, not treated as punctuation. Before this,
		// every one of these split into two words at the accent.
		{"Träffbild", "traffbild"},
		{"Åsa Öberg", "asa-oberg"},
		{"Café", "cafe"},
		{"naïve résumé", "naive-resume"},
		{"Größe", "grosse"},
		{"København", "kobenhavn"},
		{"Ærø", "aero"},
		{"Łódź", "lodz"},
		{"Þórsmörk", "thorsmork"},
		{"Ísland", "island"},

		// & reads as a word, not a gap: "R&D" is one token to a reader.
		{"R&D", "r-and-d"},

		// Nothing to transliterate: the fallback still applies rather than
		// producing an empty slug.
		{"日本語", "project"},
		{"Проект", "project"},
		{"🎯", "project"},

		// A mixed name keeps the part that has an ASCII form.
		{"Träffbild 日本", "traffbild"},
	}

	for _, tt := range tests {
		if got := Slugify(tt.input); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Every slug Slugify produces must be one the update path will accept;
// otherwise a rename derives a slug and then rejects its own derivation.
func TestSlugifyOutputIsAlwaysValid(t *testing.T) {
	inputs := []string{
		"Träffbild", "Åsa Öberg", "R&D", "日本語", "---", "", "a",
		"  -- Ærø -- ", "Größe/Höhe", "x", "9", "ß",
	}
	for _, in := range inputs {
		got := Slugify(in)
		if !validSlugRe.MatchString(got) {
			t.Errorf("Slugify(%q) = %q, which validSlugRe rejects", in, got)
		}
	}
}

func TestValidSlugRe(t *testing.T) {
	valid := []string{"my-project", "a", "abc123", "a-b", "a--b"}
	for _, s := range valid {
		if !validSlugRe.MatchString(s) {
			t.Errorf("validSlugRe should match %q", s)
		}
	}

	invalid := []string{"-leading", "trailing-", "UPPER", "has spaces", ""}
	for _, s := range invalid {
		if validSlugRe.MatchString(s) {
			t.Errorf("validSlugRe should not match %q", s)
		}
	}
}
