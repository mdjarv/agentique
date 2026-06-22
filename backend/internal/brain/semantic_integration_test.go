package brain

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/cachestore"
	"github.com/mdjarv/agentique/backend/internal/memory/chroma"
	"github.com/mdjarv/agentique/backend/internal/memory/embedhttp"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// countingDelegate wraps a real embedder and counts the texts it is asked to embed, so a live
// test can prove the warm path performs zero re-embeds without faking the vectors.
type countingDelegate struct {
	inner memory.Embedder
	texts int
}

func (c *countingDelegate) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.texts += len(texts)
	return c.inner.Embed(ctx, texts)
}

// TestWarmEmbedCacheLiveZeroReembedAfterRestart is the live proof of the cold-start fix: a first
// process indexes a corpus into Chroma, then a SECOND Service over the same collection warms its
// embedding cache from Chroma's stored vectors and re-embeds nothing for the unchanged corpus.
// It also exercises the real chroma /get bulk-vector read (GetEmbeddings/LoadVectors).
//
//	CHROMA_TEST_URL=http://127.0.0.1:8000 \
//	EMBED_TEST_URL=http://127.0.0.1:11434/v1/embeddings \
//	EMBED_TEST_MODEL=all-minilm \
//	go test ./internal/brain/ -run TestWarmEmbedCacheLiveZeroReembedAfterRestart -v
func TestWarmEmbedCacheLiveZeroReembedAfterRestart(t *testing.T) {
	chromaURL := os.Getenv("CHROMA_TEST_URL")
	embedURL := os.Getenv("EMBED_TEST_URL")
	embedModel := os.Getenv("EMBED_TEST_MODEL")
	if chromaURL == "" || embedURL == "" || embedModel == "" {
		t.Skip("set CHROMA_TEST_URL, EMBED_TEST_URL and EMBED_TEST_MODEL to run the live warm test")
	}
	ctx := context.Background()
	coll := "test_warm_" + t.Name()

	client := chroma.NewClient(chromaURL)
	if err := client.Heartbeat(ctx); err != nil {
		t.Fatalf("chroma heartbeat: %v", err)
	}

	scope := ScopeForProject("warm-test")
	corpus := []memory.Record{
		memory.New(scope, "the race detector flags concurrent map writes", memory.CategoryFact, memory.SourceConsolidated),
		memory.New(scope, "prefer modernc.org/sqlite, the pure-Go driver, over CGo", memory.CategoryFact, memory.SourceConsolidated),
		memory.New(scope, "wrap errors with %w and accumulate cleanup via errors.Join", memory.CategoryFact, memory.SourceConsolidated),
	}

	// Phase 1 ("before restart"): index the corpus into Chroma with the real embedder.
	realEmb := embedhttp.New(embedURL, embedModel)
	storeA, err := chroma.NewStore(ctx, cachestore.New(filestore.New(t.TempDir())), client, realEmb, coll)
	if err != nil {
		t.Fatalf("build store A: %v", err)
	}
	for _, r := range corpus {
		if err := storeA.Put(ctx, r); err != nil {
			t.Fatalf("index %s: %v", r.ID, err)
		}
	}

	// Phase 2 ("restart"): a fresh Service over the SAME collection, with a counting embedder so
	// any re-embed is observable. The warm reads Chroma's stored vectors via LoadVectors.
	counting := &countingDelegate{inner: realEmb}
	storeB, err := chroma.NewStore(ctx, cachestore.New(filestore.New(t.TempDir())), client, counting, coll)
	if err != nil {
		t.Fatalf("build store B: %v", err)
	}
	svcB := &Service{
		store:      storeB,
		semantic:   true,
		embedder:   counting,
		warmSrc:    storeB,
		cosThresh:  memory.DefaultSemanticThreshold,
		embedCache: make(map[string][]float32),
	}

	out, err := svcB.embedRecords(ctx, corpus)
	if err != nil {
		t.Fatalf("embedRecords after restart: %v", err)
	}
	if len(out) != len(corpus) {
		t.Fatalf("want %d vectors resolved, got %d", len(corpus), len(out))
	}
	if counting.texts != 0 {
		t.Fatalf("restart over unchanged corpus re-embedded %d texts via Chroma warm; want 0", counting.texts)
	}
	t.Logf("live restart warmed %d vectors from Chroma, re-embedded 0", len(out))
}

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
