package main

import (
	"context"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/vectortest"
)

// `brain assign-areas` goes through the service, so while the configured vector backend is
// unreachable it refuses in words instead of stamping lexical areas over semantic ones — and
// --allow-lexical is the operator's way past that.
func TestAssignAreasRefusesWhileTheVectorBackendIsUnreachable(t *testing.T) {
	ctx := context.Background()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	c.SetDown(true)
	svc, err := brain.New(ctx, brain.Config{Dir: t.TempDir(), ChromaURL: c.URL, EmbedURL: e.URL, EmbedModel: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	connectBrain(ctx, svc)
	for _, scope := range []string{"one", "two"} {
		if _, err := svc.Add(ctx, brain.ScopeForProject(scope), "the race detector runs in CI", memory.CategoryFact, memory.SourceConsolidated); err != nil {
			t.Fatal(err)
		}
	}

	_, err = assignAreasCore(ctx, svc, false)
	if err == nil || !strings.Contains(err.Error(), "--allow-lexical") {
		t.Fatalf("assign-areas while detached = %v, want a refusal naming --allow-lexical", err)
	}
	if _, err := assignAreasCore(ctx, svc, true); err != nil {
		t.Fatalf("assign-areas --allow-lexical: %v", err)
	}

	// Attached, it runs without the flag.
	c.SetDown(false)
	if err := svc.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := assignAreasCore(ctx, svc, false); err != nil {
		t.Fatalf("assign-areas while attached: %v", err)
	}
}
