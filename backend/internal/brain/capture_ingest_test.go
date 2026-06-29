package brain

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// stubExtractor returns canned candidates from Extract; Reorganize is a no-op. Enough to
// drive LearnFromTranscript deterministically without a model.
type stubExtractor struct {
	cands []memory.Candidate
}

func (e stubExtractor) Extract(_ context.Context, _ []string) ([]memory.Candidate, error) {
	return e.cands, nil
}

func (e stubExtractor) Reorganize(_ context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	return facts, nil
}

func TestLearnFromTranscriptStagesCaptures(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")
	ex := stubExtractor{cands: []memory.Candidate{
		{Text: "The build uses just.", Category: memory.CategoryFact},
		{Text: "The user prefers dark mode.", Category: memory.CategoryPreference},
	}}

	n, err := s.LearnFromTranscript(ctx, scope, []TranscriptEvent{promptEvent(t, "a session about tooling")}, ex)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("staged=%d, want 2", n)
	}

	recs, err := s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	cats := map[memory.Category]bool{}
	for _, r := range recs {
		if r.Source != memory.SourceCapture {
			t.Fatalf("record %s source=%s, want capture", r.ID, r.Source)
		}
		if r.Pinned {
			t.Fatalf("staged capture %s must not be pinned", r.ID)
		}
		cats[r.Category] = true
	}
	if !cats[memory.CategoryFact] || !cats[memory.CategoryPreference] {
		t.Fatalf("candidate categories not preserved on captures: %v", cats)
	}
}

func TestLearnFromTranscriptCapturesNotInjected(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")
	ex := stubExtractor{cands: []memory.Candidate{
		{Text: "The user always prefers tabs over spaces for indentation.", Category: memory.CategoryPreference},
	}}
	if _, err := s.LearnFromTranscript(ctx, scope, []TranscriptEvent{promptEvent(t, "x")}, ex); err != nil {
		t.Fatal(err)
	}

	block, ids := s.RecallBlock(ctx, "p1", "what does the user prefer for indentation tabs or spaces", nil)
	if strings.TrimSpace(block) != "" || len(ids) != 0 {
		t.Fatalf("captures must not be recalled: block=%q ids=%v", block, ids)
	}
	if pre := s.PinnedPreamble(ctx, "p1"); pre != "" {
		t.Fatalf("captures must not appear in the pinned preamble: %q", pre)
	}
	if oc := s.OperatingContract(ctx, "p1"); oc != "" {
		t.Fatalf("a preference capture must not enter the operating contract: %q", oc)
	}
}

func TestCaptureDoesNotPinIdentity(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	r, err := s.Capture(ctx, ScopeForProject("p1"), "User's name is Mathias.", memory.CategoryIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pinned {
		t.Fatalf("a capture must never be pinned, even CategoryIdentity")
	}
	if r.Source != memory.SourceCapture {
		t.Fatalf("source=%s, want capture", r.Source)
	}
	if r.Category != memory.CategoryIdentity {
		t.Fatalf("category=%s, want identity (preserved)", r.Category)
	}
}

func TestCaptureDefaultsCategory(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	r, err := s.Capture(ctx, memory.ScopeGlobal, "some untyped fact", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != memory.CategoryFact {
		t.Fatalf("category=%s, want fact (default)", r.Category)
	}
}

func TestCaptureConcurrentWithRecall(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Capture(ctx, scope, "secret capture text alpha beta gamma delta", memory.CategoryFact)
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			block, _ := s.RecallBlock(ctx, "p1", "alpha beta gamma delta secret", nil)
			if strings.Contains(block, "secret capture text") {
				t.Errorf("capture text leaked into a recall block: %q", block)
			}
		}()
	}
	wg.Wait()
}
