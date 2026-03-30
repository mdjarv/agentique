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
	}

	for _, tt := range tests {
		if got := Slugify(tt.input); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
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
