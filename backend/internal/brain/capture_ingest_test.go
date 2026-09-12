package brain

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// A staged capture is written but never recalled: the churn is the only path from
// capture to a fact recall will return. Before M2 this was driven through
// LearnFromTranscript; that path is gone (sessions never write facts), so the gate is
// exercised through Capture itself.
func TestCapturesAreNotRecalled(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")
	if _, err := s.Capture(ctx, scope, "The user always prefers tabs over spaces for indentation.", memory.CategoryPreference); err != nil {
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
