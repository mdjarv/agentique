package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// fakeJudge is a deterministic OutcomeJudge for the orchestration tests. It records what
// it was asked to judge and returns a canned verdict set (optionally keyed by fact id).
type fakeJudge struct {
	verdicts      []FactVerdict
	byID          map[string]FactVerdict
	gotTranscript string
	gotFacts      []JudgedFact
	calls         int
	err           error
}

func (f *fakeJudge) JudgeOutcomes(_ context.Context, transcript string, facts []JudgedFact) ([]FactVerdict, error) {
	f.calls++
	f.gotTranscript = transcript
	f.gotFacts = facts
	if f.err != nil {
		return nil, f.err
	}
	if f.byID != nil {
		out := make([]FactVerdict, 0, len(facts))
		for _, fact := range facts {
			if v, ok := f.byID[fact.ID]; ok {
				v.ID = fact.ID
				out = append(out, v)
			}
		}
		return out, nil
	}
	return f.verdicts, f.err
}

// brainBlock formats a <brain> envelope exactly as RecallBlock persists it.
func brainBlock(ids ...string) string {
	var b strings.Builder
	b.WriteString("<brain>\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "  <fact id=%q>some recalled fact</fact>\n", id)
	}
	b.WriteString("</brain>")
	return b.String()
}

// promptEvent builds a persisted "prompt" event whose Data holds the (already augmented)
// turn prompt — the same shape Session.persistQueryStart writes.
func promptEvent(t *testing.T, text string) TranscriptEvent {
	t.Helper()
	b, err := json.Marshal(map[string]string{"prompt": text})
	if err != nil {
		t.Fatal(err)
	}
	return TranscriptEvent{Type: "prompt", Data: string(b)}
}

func textEvent(t *testing.T, content string) TranscriptEvent {
	t.Helper()
	b, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	return TranscriptEvent{Type: "text", Data: string(b)}
}

func TestExtractInjectedFactIDs(t *testing.T) {
	events := []TranscriptEvent{
		promptEvent(t, brainBlock("id-a", "id-b")+"\n\nhow do migrations work?"),
		textEvent(t, "Migrations use goose."),
		// A second turn re-surfaces id-b (delta recall normally prevents this, but the
		// extractor must de-dupe defensively) and adds id-c.
		promptEvent(t, brainBlock("id-b", "id-c")+"\n\nand the frontend?"),
		// Non-prompt event with a stray <fact ...> in assistant prose must be ignored.
		textEvent(t, `I wrote <fact id="not-real">x</fact> in my answer`),
	}
	got := extractInjectedFactIDs(events)
	want := []string{"id-a", "id-b", "id-c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestOutcomeTranscriptStripsBrainBlocks(t *testing.T) {
	events := []TranscriptEvent{
		promptEvent(t, brainBlock("id-a")+"\n\nplease build the thing"),
		textEvent(t, "Building it now."),
	}
	tr := outcomeTranscript(events)
	if strings.Contains(tr, "<brain>") || strings.Contains(tr, "<fact") {
		t.Fatalf("brain envelope should be stripped from judge transcript, got:\n%s", tr)
	}
	if !strings.Contains(tr, "please build the thing") || !strings.Contains(tr, "Building it now.") {
		t.Fatalf("transcript should keep the conversation, got:\n%s", tr)
	}
}

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestApplyOutcomesFromTranscript(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")

	helped, err := s.Add(ctx, p1, "Database migrations use goose numbering.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	contra, err := s.Add(ctx, p1, "The build uses make.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	neutral, err := s.Add(ctx, p1, "Frontend uses tailwind.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	// A fact in ANOTHER project, surfaced (impossibly) in p1's transcript: the scope guard
	// must refuse to rate it, and it must not even reach the judge.
	other, err := s.Add(ctx, ScopeForProject("p2"), "p2 only fact.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	events := []TranscriptEvent{
		promptEvent(t, brainBlock(helped.ID, contra.ID, neutral.ID, other.ID)+"\n\nset up the project"),
		textEvent(t, "Running goose migrations. Note: the build is actually `just`, not make."),
	}

	judge := &fakeJudge{byID: map[string]FactVerdict{
		helped.ID:  {Verdict: OutcomeHelped},
		contra.ID:  {Verdict: OutcomeContradicted, Reason: "build is just, not make"},
		neutral.ID: {Verdict: OutcomeNeutral},
		// A verdict for an id that was never surfaced — must be ignored (anti-hallucination).
		"hallucinated-id": {Verdict: OutcomeHelped},
	}}

	rep, err := s.ApplyOutcomesFromTranscript(ctx, p1, events, judge)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 3 {
		t.Fatalf("judged = %d, want 3 (p2 fact excluded by scope guard)", rep.Judged)
	}
	if rep.Helped != 1 || rep.Flagged != 1 {
		t.Fatalf("report = %+v, want helped=1 flagged=1", rep)
	}

	// The p2 fact must never have been offered to the judge.
	for _, f := range judge.gotFacts {
		if f.ID == other.ID {
			t.Fatalf("scope guard failed: p2 fact %s reached the judge", other.ID)
		}
	}

	// helped → MarkAutoHelped: Helped=1, gentle gap-close 0.8 → 0.8375.
	gotHelped, err := s.Get(ctx, helped.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantScore := 0.8 + memory.AutoCorroborationGapClose*(memory.CorroborationCeiling-0.8)
	if gotHelped.Helped != 1 || !approxEq(gotHelped.ConfidenceScore, wantScore) {
		t.Fatalf("helped fact = {Helped:%d Score:%.4f}, want {Helped:1 Score:%.4f}", gotHelped.Helped, gotHelped.ConfidenceScore, wantScore)
	}

	// contradicted → Flag: review band + auto-marked reason.
	gotContra, err := s.Get(ctx, contra.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(gotContra.ConfidenceScore, memory.ContradictedScore) {
		t.Fatalf("contradicted score = %.4f, want %.4f", gotContra.ConfidenceScore, memory.ContradictedScore)
	}
	if !strings.HasPrefix(gotContra.ReviewNote, "auto:") {
		t.Fatalf("contradicted review note should be auto-marked, got %q", gotContra.ReviewNote)
	}

	// neutral and the p2 fact must be untouched.
	gotNeutral, _ := s.Get(ctx, neutral.ID)
	if gotNeutral.Helped != 0 || gotNeutral.ReviewNote != "" || !approxEq(gotNeutral.ConfidenceScore, 0.8) {
		t.Fatalf("neutral fact must be unchanged, got %+v", gotNeutral)
	}
	gotOther, _ := s.Get(ctx, other.ID)
	if gotOther.Helped != 0 || gotOther.ReviewNote != "" {
		t.Fatalf("out-of-scope fact must be unchanged, got %+v", gotOther)
	}
}

func TestApplyOutcomesNoInjectedFactsSkipsJudge(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	events := []TranscriptEvent{
		promptEvent(t, "no recall here, just a plain prompt"),
		textEvent(t, "done"),
	}
	judge := &fakeJudge{}
	rep, err := s.ApplyOutcomesFromTranscript(ctx, ScopeForProject("p1"), events, judge)
	if err != nil {
		t.Fatal(err)
	}
	if judge.calls != 0 {
		t.Fatalf("judge must not be called when nothing was injected, calls=%d", judge.calls)
	}
	if rep != (OutcomeReport{}) {
		t.Fatalf("empty report expected, got %+v", rep)
	}
}

func TestApplyOutcomesDeletedFactSkipped(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")
	// Reference an id that no longer exists (consolidated/deleted since it was surfaced).
	events := []TranscriptEvent{promptEvent(t, brainBlock("gone-id")+"\n\nwork")}
	judge := &fakeJudge{byID: map[string]FactVerdict{"gone-id": {Verdict: OutcomeHelped}}}
	rep, err := s.ApplyOutcomesFromTranscript(ctx, p1, events, judge)
	if err != nil {
		t.Fatal(err)
	}
	if judge.calls != 0 || rep.Judged != 0 {
		t.Fatalf("a since-deleted fact must not be judged, calls=%d report=%+v", judge.calls, rep)
	}
}

func TestMarkAutoHelpedIsGentlerThanExplicit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")

	auto, err := s.Add(ctx, p1, "auto fact", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := s.Add(ctx, p1, "explicit fact", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.MarkAutoHelped(ctx, auto.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkHelped(ctx, explicit.ID); err != nil {
		t.Fatal(err)
	}

	gotAuto, _ := s.Get(ctx, auto.ID)
	gotExplicit, _ := s.Get(ctx, explicit.ID)

	if !approxEq(gotAuto.ConfidenceScore, 0.8375) {
		t.Fatalf("auto-helped score = %.4f, want 0.8375", gotAuto.ConfidenceScore)
	}
	if !approxEq(gotExplicit.ConfidenceScore, 0.875) {
		t.Fatalf("explicit-helped score = %.4f, want 0.875", gotExplicit.ConfidenceScore)
	}
	if !(gotAuto.ConfidenceScore < gotExplicit.ConfidenceScore) {
		t.Fatalf("auto (%.4f) must move trust less than explicit (%.4f)", gotAuto.ConfidenceScore, gotExplicit.ConfidenceScore)
	}
	// Both still record a Helped outcome and the recency stamp.
	if gotAuto.Helped != 1 || gotExplicit.Helped != 1 {
		t.Fatalf("both should increment Helped, got auto=%d explicit=%d", gotAuto.Helped, gotExplicit.Helped)
	}
	if gotAuto.LastUsedAt.IsZero() {
		t.Fatalf("auto-helped should stamp LastUsedAt")
	}
}

func TestClaudeOutcomeJudgeParsesVerdicts(t *testing.T) {
	structured := `{"verdicts":[` +
		`{"id":"a","verdict":"helped"},` +
		`{"id":"b","verdict":"contradicted","reason":"user said otherwise"},` +
		`{"id":"c","verdict":"neutral"},` +
		`{"id":"d","verdict":"GARBAGE"},` +
		`{"id":"  ","verdict":"helped"}]}`
	j := NewClaudeOutcomeJudge(fakeRunner{structured: structured}, "haiku")
	out, err := j.JudgeOutcomes(context.Background(), "User: do x\nAssistant: did x",
		[]JudgedFact{{ID: "a", Text: "fa"}, {ID: "b", Text: "fb"}, {ID: "c", Text: "fc"}, {ID: "d", Text: "fd"}})
	if err != nil {
		t.Fatal(err)
	}
	// The blank-id verdict is dropped; the GARBAGE verdict normalizes to neutral.
	if len(out) != 4 {
		t.Fatalf("want 4 verdicts (blank id dropped), got %d: %+v", len(out), out)
	}
	byID := map[string]FactVerdict{}
	for _, v := range out {
		byID[v.ID] = v
	}
	if byID["a"].Verdict != OutcomeHelped {
		t.Fatalf("a should be helped, got %q", byID["a"].Verdict)
	}
	if byID["b"].Verdict != OutcomeContradicted || byID["b"].Reason != "user said otherwise" {
		t.Fatalf("b should be contradicted with reason, got %+v", byID["b"])
	}
	if byID["c"].Verdict != OutcomeNeutral {
		t.Fatalf("c should be neutral, got %q", byID["c"].Verdict)
	}
	if byID["d"].Verdict != OutcomeNeutral {
		t.Fatalf("unknown verdict must normalize to neutral, got %q", byID["d"].Verdict)
	}
}

func TestAutoFlagReason(t *testing.T) {
	if got := autoFlagReason(""); !strings.HasPrefix(got, "auto:") {
		t.Fatalf("empty reason should still be auto-marked, got %q", got)
	}
	if got := autoFlagReason("build is just"); got != "auto: build is just" {
		t.Fatalf("reason should be auto-prefixed, got %q", got)
	}
}
