package brain

import (
	"context"
	"testing"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

type fakeRunner struct {
	text string
	err  error
}

func (f fakeRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &claudecli.BlockingResult{Text: f.text}, nil
}

func TestHaikuExtractorExtract(t *testing.T) {
	// Response wrapped in prose + a code fence + a reasoning block — must still parse.
	resp := "<think>let me think</think>\nHere are the facts:\n```json\n" +
		`[{"text":"Build runs via just","category":"project"},{"text":"User prefers concise replies","category":"preference"}]` +
		"\n```\n"
	ex := NewHaikuExtractor(fakeRunner{text: resp})
	cands, err := ex.Extract(context.Background(), []string{"User: how do I build?\nAssistant: use just"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d: %+v", len(cands), cands)
	}
	if cands[0].Category != memory.CategoryProject || cands[1].Category != memory.CategoryPreference {
		t.Fatalf("categories wrong: %+v", cands)
	}
}

func TestHaikuExtractorToleratesGarbage(t *testing.T) {
	ex := NewHaikuExtractor(fakeRunner{text: "I could not find any durable facts."})
	cands, err := ex.Extract(context.Background(), []string{"some transcript"})
	if err != nil {
		t.Fatalf("garbage response must not error: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("want 0 candidates, got %+v", cands)
	}
}

func TestHaikuExtractorEmptyEpisodes(t *testing.T) {
	ex := NewHaikuExtractor(fakeRunner{text: "should not be called"})
	cands, err := ex.Extract(context.Background(), []string{"   ", ""})
	if err != nil || len(cands) != 0 {
		t.Fatalf("empty episodes should yield nothing, got %v %v", cands, err)
	}
}

func TestHaikuExtractorReorganize(t *testing.T) {
	// keep 'a' rewritten, drop 'b', add one abstraction (empty id)
	resp := `[{"id":"a","text":"clarified a","category":"fact"},{"id":"","text":"general rule","category":"preference"}]`
	ex := NewHaikuExtractor(fakeRunner{text: resp})
	out, err := ex.Reorganize(context.Background(), []memory.Fact{
		{ID: "a", Text: "vague a", Category: memory.CategoryFact},
		{ID: "b", Text: "fact b", Category: memory.CategoryFact},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 facts, got %+v", out)
	}
	if out[0].ID != "a" || out[0].Text != "clarified a" {
		t.Fatalf("kept fact wrong: %+v", out[0])
	}
	if out[1].ID != "" {
		t.Fatalf("abstraction should have empty id: %+v", out[1])
	}
}

func TestHaikuExtractorReorganizeGarbageIsNoOp(t *testing.T) {
	facts := []memory.Fact{{ID: "a", Text: "fact a", Category: memory.CategoryFact}}
	ex := NewHaikuExtractor(fakeRunner{text: "nonsense"})
	out, err := ex.Reorganize(context.Background(), facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("garbage reorganize must be a no-op, got %+v", out)
	}
}

func TestParseJSONArray(t *testing.T) {
	cases := map[string]string{
		"```json\n[1,2]\n```":             "[1,2]",
		"<think>x</think>[3]":             "[3]",
		"prose before [4] prose after":   "[4]",
		"no array here":                  "[]",
		"[{\"a\":1}]":                    "[{\"a\":1}]",
	}
	for in, want := range cases {
		if got := string(parseJSONArray(in)); got != want {
			t.Errorf("parseJSONArray(%q)=%q want %q", in, got, want)
		}
	}
}
