package brain

import (
	"context"
	"sync"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func TestAddReinforcesDurableDuplicate(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")

	r1, err := s.Add(ctx, scope, "The deploy script lives in scripts/deploy.sh.", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Corroborations != 0 {
		t.Fatalf("fresh add Corroborations=%d, want 0", r1.Corroborations)
	}
	base := r1.ConfidenceScore

	r2, err := s.Add(ctx, scope, "the deploy script lives in scripts/deploy.sh", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ID != r1.ID {
		t.Fatalf("reinforce should return the same record: %s vs %s", r1.ID, r2.ID)
	}
	if r2.Corroborations != 1 {
		t.Fatalf("Corroborations=%d, want 1", r2.Corroborations)
	}
	if !(r2.ConfidenceScore > base) {
		t.Fatalf("score did not rise on reinforce: %v <= %v", r2.ConfidenceScore, base)
	}

	recs, err := s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("reinforce must not add a record: got %d", len(recs))
	}
	got, err := s.Get(ctx, r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Corroborations != 1 {
		t.Fatalf("persisted Corroborations=%d, want 1", got.Corroborations)
	}
}

func TestAddDoesNotReinforceCaptureOnlyMatch(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")

	cap1, err := s.Capture(ctx, scope, "An ephemeral observation about the cache layer.", memory.CategoryFact)
	if err != nil {
		t.Fatal(err)
	}
	// Add of the SAME text must create a NEW durable record (captures are never dedup targets).
	add, err := s.Add(ctx, scope, "An ephemeral observation about the cache layer.", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if add.ID == cap1.ID {
		t.Fatalf("Add must not reinforce a capture-only match")
	}
	if add.Source != memory.SourceAgent {
		t.Fatalf("Add source=%s, want agent (a durable write)", add.Source)
	}
}

func TestCaptureReinforcesDurableDuplicate(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")

	dur, err := s.Add(ctx, scope, "The primary database is sqlite.", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	// A capture duplicating a DURABLE fact reinforces it; no capture is written.
	rc, err := s.Capture(ctx, scope, "the primary database is sqlite", memory.CategoryFact)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ID != dur.ID {
		t.Fatalf("capture dup should reinforce the durable fact: %s vs %s", rc.ID, dur.ID)
	}
	if rc.Corroborations != 1 {
		t.Fatalf("Corroborations=%d, want 1", rc.Corroborations)
	}
	recs, err := s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("capture-dup must not add a record: got %d", len(recs))
	}

	// Genuinely-new capture text stages exactly one capture.
	if _, err := s.Capture(ctx, scope, "A wholly new fact about request routing.", memory.CategoryFact); err != nil {
		t.Fatal(err)
	}
	recs, err = s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	captures := 0
	for _, r := range recs {
		if r.Source == memory.SourceCapture {
			captures++
		}
	}
	if len(recs) != 2 || captures != 1 {
		t.Fatalf("new capture should add exactly one SourceCapture record: recs=%d captures=%d", len(recs), captures)
	}
}

func TestAddCaptureReinforceRace(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	scope := ScopeForProject("p1")

	seed, err := s.Add(ctx, scope, "The shared durable fact under contention.", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}

	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.Add(ctx, scope, "the shared durable fact under contention", memory.CategoryFact, memory.SourceAgent)
		}()
		go func() {
			defer wg.Done()
			_, _ = s.Capture(ctx, scope, "the shared durable fact under contention", memory.CategoryFact)
		}()
	}
	wg.Wait()

	// s.mu serializes every reinforce, so no increment is lost: exactly 2n corroborations.
	got, err := s.Get(ctx, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Corroborations != 2*n {
		t.Fatalf("Corroborations=%d, want %d (no lost increments under s.mu)", got.Corroborations, 2*n)
	}
	recs, err := s.List(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("contention must not create new records: got %d", len(recs))
	}
}
