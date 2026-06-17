package brain

import (
	"context"
	"encoding/json"
	"testing"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

type fakeRunner struct {
	text       string
	structured string // when set, returned as BlockingResult.StructuredOutput
	err        error
}

func (f fakeRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	res := &claudecli.BlockingResult{Text: f.text}
	if f.structured != "" {
		res.StructuredOutput = json.RawMessage(f.structured)
	}
	return res, nil
}

func newExtractor(r fakeRunner) *ClaudeExtractor {
	return NewClaudeExtractor(r, claudecli.ModelHaiku)
}

func TestParseModel(t *testing.T) {
	for name, want := range map[string]claudecli.Model{
		"haiku": claudecli.ModelHaiku, "Sonnet": claudecli.ModelSonnet, " opus ": claudecli.ModelOpus,
	} {
		got, err := ParseModel(name)
		if err != nil || got != want {
			t.Fatalf("ParseModel(%q)=%q,%v want %q", name, got, err, want)
		}
	}
	if _, err := ParseModel("gpt-9"); err == nil {
		t.Fatal("unknown model must error, not fall through to a default")
	}
}

// Schema-constrained happy path: output arrives in StructuredOutput as the
// object wrapper the schema requests.
func TestExtractStructuredOutput(t *testing.T) {
	structured := `{"memories":[{"text":"Build runs via just","category":"project"},{"text":"User prefers concise replies","category":"preference"}]}`
	cands, err := newExtractor(fakeRunner{structured: structured}).Extract(context.Background(), []string{"User: how do I build?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 || cands[0].Category != memory.CategoryProject || cands[1].Category != memory.CategoryPreference {
		t.Fatalf("structured extract wrong: %+v", cands)
	}
}

// Fallback path: no StructuredOutput, a bare array buried in prose + a code
// fence + a reasoning block must still parse.
func TestExtractTextFallback(t *testing.T) {
	resp := "<think>let me think</think>\nHere are the facts:\n```json\n" +
		`[{"text":"Build runs via just","category":"project"},{"text":"User prefers concise replies","category":"preference"}]` +
		"\n```\n"
	cands, err := newExtractor(fakeRunner{text: resp}).Extract(context.Background(), []string{"User: how do I build?"})
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

func TestExtractToleratesGarbage(t *testing.T) {
	cands, err := newExtractor(fakeRunner{text: "I could not find any durable facts."}).Extract(context.Background(), []string{"some transcript"})
	if err != nil {
		t.Fatalf("garbage response must not error: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("want 0 candidates, got %+v", cands)
	}
}

func TestExtractEmptyEpisodes(t *testing.T) {
	cands, err := newExtractor(fakeRunner{text: "should not be called"}).Extract(context.Background(), []string{"   ", ""})
	if err != nil || len(cands) != 0 {
		t.Fatalf("empty episodes should yield nothing, got %v %v", cands, err)
	}
}

func TestReorganizeStructuredOutput(t *testing.T) {
	// keep 'a' rewritten, drop 'b', add one abstraction (empty id)
	structured := `{"facts":[{"id":"a","text":"clarified a","category":"fact"},{"id":"","text":"general rule","category":"preference"}]}`
	out, err := newExtractor(fakeRunner{structured: structured}).Reorganize(context.Background(), []memory.Fact{
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

// Bare-array text fallback for Reorganize (model dropped the object wrapper).
func TestReorganizeTextFallback(t *testing.T) {
	resp := `[{"id":"a","text":"clarified a","category":"fact"}]`
	out, err := newExtractor(fakeRunner{text: resp}).Reorganize(context.Background(), []memory.Fact{
		{ID: "a", Text: "vague a", Category: memory.CategoryFact},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "a" || out[0].Text != "clarified a" {
		t.Fatalf("text-fallback reorganize wrong: %+v", out)
	}
}

func TestReorganizeGarbageIsNoOp(t *testing.T) {
	facts := []memory.Fact{{ID: "a", Text: "fact a", Category: memory.CategoryFact}}
	out, err := newExtractor(fakeRunner{text: "nonsense"}).Reorganize(context.Background(), facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("garbage reorganize must be a no-op, got %+v", out)
	}
}

// An explicit empty result must not be read as "delete everything".
func TestReorganizeEmptyIsNoOp(t *testing.T) {
	facts := []memory.Fact{{ID: "a", Text: "fact a", Category: memory.CategoryFact}}
	out, err := newExtractor(fakeRunner{structured: `{"facts":[]}`}).Reorganize(context.Background(), facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("empty reorganize must be a no-op, got %+v", out)
	}
}

// Chunking must never drop facts: when every chunk's model call no-ops, the full
// input set is returned so the consolidation core deletes nothing.
func TestReorganizeChunkingPreservesAllOnNoOp(t *testing.T) {
	orig := maxReorgBatch
	maxReorgBatch = 2
	defer func() { maxReorgBatch = orig }()

	facts := []memory.Fact{
		{ID: "a", Text: "a", Category: memory.CategoryFact},
		{ID: "b", Text: "b", Category: memory.CategoryFact},
		{ID: "c", Text: "c", Category: memory.CategoryFact},
		{ID: "d", Text: "d", Category: memory.CategoryFact},
		{ID: "e", Text: "e", Category: memory.CategoryFact},
	}
	out, err := newExtractor(fakeRunner{text: "garbage"}).Reorganize(context.Background(), facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(facts) {
		t.Fatalf("chunked no-op must preserve all %d facts, got %d: %+v", len(facts), len(out), out)
	}
}

func TestParseJSONArray(t *testing.T) {
	cases := map[string]string{
		"```json\n[1,2]\n```":           "[1,2]",
		"<think>x</think>[3]":           "[3]",
		"prose before [4] prose after": "[4]",
		"no array here":                "[]",
		"[{\"a\":1}]":                  "[{\"a\":1}]",
	}
	for in, want := range cases {
		if got := string(parseJSONArray(in)); got != want {
			t.Errorf("parseJSONArray(%q)=%q want %q", in, got, want)
		}
	}
}
