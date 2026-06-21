package memory

import "testing"

// TestTokenizeDropsConversationalFiller guards the precision fix: conversational and
// instructional filler must be dropped so a natural query ("I'd like you to…") can't
// spuriously match a fact that merely contains the same filler word, while content
// tokens survive.
func TestTokenizeDropsConversationalFiller(t *testing.T) {
	got := tokenSet("I'd like you to please help me investigate whether windows is possible")

	for _, filler := range []string{"like", "please", "help", "you", "whether"} {
		if _, ok := got[filler]; ok {
			t.Errorf("filler %q should be a stopword, but survived", filler)
		}
	}
	for _, keep := range []string{"investigate", "windows", "possible"} {
		if _, ok := got[keep]; !ok {
			t.Errorf("content token %q should survive tokenization", keep)
		}
	}
}
