package brain

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// TestBrainSemanticWiring validates the PRODUCTION entry point end-to-end against a live
// Chroma + embedding endpoint: Config -> New detects the env, enables semantic mode, builds
// the Chroma-backed store, and threads the veto + vouch (cosThresh) scores into Recall so
// the real github mis-recall is excluded via the vector path. This covers the brain.New
// wiring the memory/chroma integration test bypasses (it constructs the store directly).
//
//	CHROMA_TEST_URL=http://127.0.0.1:8000 \
//	EMBED_TEST_URL=http://127.0.0.1:11434/v1/embeddings \
//	EMBED_TEST_MODEL=all-minilm \
//	go test ./internal/brain/ -run TestBrainSemanticWiring -v
func TestBrainSemanticWiring(t *testing.T) {
	chromaURL := os.Getenv("CHROMA_TEST_URL")
	embedURL := os.Getenv("EMBED_TEST_URL")
	embedModel := os.Getenv("EMBED_TEST_MODEL")
	if chromaURL == "" || embedURL == "" || embedModel == "" {
		t.Skip("set CHROMA_TEST_URL, EMBED_TEST_URL and EMBED_TEST_MODEL to run the live brain wiring test")
	}
	ctx := context.Background()

	svc, err := New(ctx, Config{
		Dir:        t.TempDir(),
		ChromaURL:  chromaURL,
		EmbedURL:   embedURL,
		EmbedModel: embedModel,
		Collection: "test_brain_" + t.Name(),
		// SemanticThreshold / VectorVetoScore left 0 → shipped defaults (0.45 / 0.15).
	})
	if err != nil {
		t.Fatalf("brain.New: %v", err)
	}
	if !svc.SemanticEnabled() {
		t.Fatal("semantic mode should be enabled when Chroma + embedder are reachable")
	}

	scope := ScopeForProject("meta-spec")
	add := func(text string, cat memory.Category) {
		if _, err := svc.Add(ctx, scope, text, cat, memory.SourceConsolidated); err != nil {
			t.Fatalf("add %q: %v", text, err)
		}
	}
	add("Private allbin Go modules require GOPRIVATE=github.com/allbin/* plus git SSH config", memory.CategoryFact)
	add("the release workflow pushes build artifacts to github actions", memory.CategoryFact)
	add("Sentry for the Vite TS sub-repo reads its DSN from environment secrets and vars", memory.CategoryProject)

	res, err := svc.Recall(ctx, recallScopes(scope), "secrets and vars on github", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var texts []string
	for _, r := range res.Recalled {
		texts = append(texts, r.Text)
		if strings.Contains(r.Text, "GOPRIVATE") {
			t.Fatalf("brain recall leaked the off-topic Go/GOPRIVATE fact via the live vector path: %v", texts)
		}
	}
	t.Logf("brain recall (semantic, shipped defaults) -> %v", texts)
}
