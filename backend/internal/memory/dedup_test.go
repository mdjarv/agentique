package memory

import "testing"

func TestIsTextDuplicate(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Use just, not npx", "  use just, not npx  ", true},        // exact (case/space)
		{"use just never raw npx", "use just always raw npx", true}, // high jaccard
		{"use just npx", "completely different sentence here", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := IsTextDuplicate(c.a, c.b, DefaultDuplicateThreshold); got != c.want {
			t.Errorf("IsTextDuplicate(%q,%q)=%v want %v (jaccard=%.2f)", c.a, c.b, got, c.want, jaccard(c.a, c.b))
		}
	}
}

func TestFindDuplicate(t *testing.T) {
	existing := []Record{
		rec("a", "the auth flow lives in internal auth", CategoryProject),
		rec("b", "user prefers dark mode", CategoryPreference),
	}
	if _, ok := FindDuplicate("user prefers dark mode", existing, DefaultDuplicateThreshold); !ok {
		t.Fatal("expected duplicate of 'b'")
	}
	if _, ok := FindDuplicate("entirely novel unrelated fact", existing, DefaultDuplicateThreshold); ok {
		t.Fatal("did not expect a duplicate")
	}
}
