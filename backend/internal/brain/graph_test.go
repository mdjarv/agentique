package brain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// brec builds a durable record with explicit confidence + related edges for report tests.
func brec(id string, score float64, src memory.Source, related ...string) memory.Record {
	r := memory.New(memory.ScopeGlobal, "fact "+id, memory.CategoryFact, src)
	r.ID = id
	r.ConfidenceScore = score
	r = memory.NormalizeConfidence(r)
	r.Related = related
	return r
}

func TestBuildReportGodNodesBridgesAndConfirm(t *testing.T) {
	// Path c1—hub—c2 plus two extra leaves on hub: hub is the god node + the bridge.
	recs := []memory.Record{
		brec("hub", memory.DefaultInferredScore, memory.SourceConsolidated, "c1", "c2", "l1", "l2"),
		brec("c1", memory.DefaultInferredScore, memory.SourceConsolidated, "hub"),
		brec("c2", memory.DefaultInferredScore, memory.SourceConsolidated, "hub"),
		brec("l1", memory.DefaultInferredScore, memory.SourceConsolidated, "hub"),
		brec("l2", memory.DefaultInferredScore, memory.SourceConsolidated, "hub"),
		// A low-confidence cross-project generalization (isolated) → confirm queue + isolated.
		brec("xp", memory.CrossProjectInferredScore, memory.SourceConsolidated),
		// A human ground-truth fact at low score should NOT be confirmable.
		brec("human", 0.4, memory.SourceHuman),
	}
	cent := memory.ComputeCentrality(recs)
	rep := buildReport(recs, cent, time.Now().UTC())

	if len(rep.GodNodes) == 0 || rep.GodNodes[0] != "hub" {
		t.Errorf("god nodes = %v, want hub first", rep.GodNodes)
	}
	if len(rep.Bridges) == 0 || rep.Bridges[0] != "hub" {
		t.Errorf("bridges = %v, want hub first", rep.Bridges)
	}
	// Only the cross-project fact needs confirmation; the human fact is protected,
	// the inferred 0.8 facts are above the confirmation score.
	if len(rep.NeedsConfirmation) != 1 || rep.NeedsConfirmation[0] != "xp" {
		t.Errorf("needsConfirmation = %v, want [xp]", rep.NeedsConfirmation)
	}
	// Isolated = degree 0: the cross-project fact and the human fact (no edges).
	iso := map[string]bool{}
	for _, id := range rep.Isolated {
		iso[id] = true
	}
	if !iso["xp"] || !iso["human"] {
		t.Errorf("isolated = %v, want to include xp and human", rep.Isolated)
	}
	if iso["hub"] {
		t.Errorf("hub should not be isolated, got %v", rep.Isolated)
	}
}

func TestHandleGraphEndToEnd(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	// Two distinct facts (not near-duplicates, so neither is deduped on Add).
	a, _ := svc.Add(ctx, memory.ScopeGlobal, "the project builds with just", memory.CategoryProject, memory.SourceAgent)
	b, _ := svc.Add(ctx, memory.ScopeGlobal, "frontend path alias maps to source", memory.CategoryProject, memory.SourceAgent)
	// Rebuild the link graph (no edge expected here; exercises the relink path).
	if _, err := memory.RelinkScope(ctx, svc.store, memory.ScopeGlobal); err != nil {
		t.Fatal(err)
	}

	h := &Handler{Service: svc}
	req := httptest.NewRequest(http.MethodGet, "/api/brain/graph", nil)
	w := httptest.NewRecorder()
	if err := h.HandleGraph(w, req); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got graphDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(got.Nodes))
	}
	// Every node carries a confidence tier + score from the DTO.
	for _, n := range got.Nodes {
		if n.Confidence == "" || n.ConfidenceScore <= 0 {
			t.Errorf("node %s missing confidence: %+v", n.ID, n)
		}
	}
	_ = a
	_ = b
}

func TestConfirmMakesGroundTruth(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	rec, err := svc.Add(ctx, memory.ScopeGlobal, "a generalized preference", memory.CategoryPreference, memory.SourceConsolidated)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Confidence != memory.ConfidenceInferred {
		t.Fatalf("seed confidence = %s, want inferred", rec.Confidence)
	}
	got, err := svc.Confirm(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != memory.SourceHuman || got.Confidence != memory.ConfidenceExtracted || got.ConfidenceScore != memory.ScoreGroundTruth {
		t.Errorf("after confirm = (%s,%s,%v), want human/extracted/ground-truth", got.Source, got.Confidence, got.ConfidenceScore)
	}
	// Confirmed facts are no longer in the confirm queue.
	all, _ := svc.List(ctx, memory.ScopeGlobal)
	cent := memory.ComputeCentrality(all)
	if rep := buildReport(all, cent, time.Now().UTC()); len(rep.NeedsConfirmation) != 0 {
		t.Errorf("needsConfirmation after confirm = %v, want empty", rep.NeedsConfirmation)
	}
}
