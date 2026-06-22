package chroma

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/embedhttp"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// TestSemanticRecallVetoesGithubMisRecall reproduces the real meta-spec mis-recall
// (docs/brain-semantic-recall.md) end-to-end against a LIVE Chroma + embedding endpoint,
// to (a) measure the actual cosine distribution for the chosen model and (b) prove the
// vector veto (priority #1) excludes the off-topic Go/GOPRIVATE fact through the real
// vector path, not just the lexical guard.
//
// Run with a local Ollama all-minilm + Chroma:
//
//	CHROMA_TEST_URL=http://127.0.0.1:8000 \
//	EMBED_TEST_URL=http://127.0.0.1:11434/v1/embeddings \
//	EMBED_TEST_MODEL=all-minilm \
//	go test ./internal/memory/chroma/ -run TestSemanticRecallVetoesGithubMisRecall -v
func TestSemanticRecallVetoesGithubMisRecall(t *testing.T) {
	chromaURL := os.Getenv("CHROMA_TEST_URL")
	embedURL := os.Getenv("EMBED_TEST_URL")
	embedModel := os.Getenv("EMBED_TEST_MODEL")
	if chromaURL == "" || embedURL == "" || embedModel == "" {
		t.Skip("set CHROMA_TEST_URL, EMBED_TEST_URL and EMBED_TEST_MODEL to run the live semantic recall test")
	}
	ctx := context.Background()
	client := NewClient(chromaURL)
	if err := client.Heartbeat(ctx); err != nil {
		t.Fatalf("chroma heartbeat: %v", err)
	}
	emb := embedhttp.New(embedURL, embedModel)

	base := filestore.New(t.TempDir())
	coll := fmt.Sprintf("test_sem_%d", time.Now().UnixNano())
	st, err := NewStore(ctx, base, client, emb, coll, WithErrorHandler(func(e error) {
		t.Errorf("unexpected index error: %v", e)
	}))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	const scope = memory.Scope("project:meta-spec")
	mk := func(id, text string, cat memory.Category) memory.Record {
		r := memory.New(scope, text, cat, memory.SourceConsolidated)
		r.ID = id
		return r
	}
	// The corpus mirrors the real scope: the off-topic Go fact that wrongly recalled, a
	// 2nd github mention (so the lexical df of "github" > 1), the genuinely on-topic
	// answer (Sentry secrets/vars), and unrelated noise.
	recs := []memory.Record{
		mk("go", "Private allbin Go modules require GOPRIVATE=github.com/allbin/* plus git SSH config", memory.CategoryFact),
		mk("ci", "the release workflow pushes build artifacts to github actions", memory.CategoryFact),
		mk("sentry", "Sentry for the Vite TS sub-repo reads its DSN from environment secrets and vars", memory.CategoryProject),
		mk("nginx", "nginx proxy manager terminates TLS with Let's Encrypt", memory.CategoryFact),
		mk("sqlite", "prefer modernc.org/sqlite (pure Go, no CGo) for SQLite", memory.CategoryPreference),
	}
	for _, r := range recs {
		if err := st.Put(ctx, r); err != nil {
			t.Fatalf("put %s: %v", r.ID, err)
		}
	}

	const query = "secrets and vars on github"

	// --- Measurement: raw vector scores the model assigns this query per fact. This is
	// the calibration data for DefaultVectorVetoScore (model-specific). ---
	hits, err := st.Search(ctx, query, []memory.Scope{scope}, len(recs))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	score := map[string]float64{}
	for _, h := range hits {
		score[h.ID] = h.Score
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	t.Logf("model=%s query=%q vector scores (cosine, high=related):", embedModel, query)
	for _, h := range hits {
		t.Logf("  %-7s %.4f", h.ID, h.Score)
	}

	// The whole hypothesis: the embedder ranks the on-topic Sentry fact ABOVE the
	// off-topic Go/GOPRIVATE fact for this query — i.e. semantics separates them where
	// the lone "github" token could not.
	if score["sentry"] <= score["go"] {
		t.Fatalf("expected sentry (on-topic) to outscore go (off-topic): sentry=%.4f go=%.4f", score["sentry"], score["go"])
	}
	t.Logf("separation: sentry=%.4f > go=%.4f (margin %.4f)", score["sentry"], score["go"], score["sentry"]-score["go"])

	// --- Production defaults: the SHIPPED config (VetoScore 0 -> 0.15, VouchScore 0 ->
	// DefaultSemanticThreshold 0.45) must exclude the off-topic Go fact via the real
	// vector path. The Go fact (vs ~0.36) clears the veto floor but does NOT vouch for its
	// lone "github" keyword match (0.36 < 0.45), so the vouch-gated lexical guard drops it
	// — no fragile hand-set floor needed. This is the session's end-to-end deliverable. ---
	res, err := memory.Recall(ctx, st, memory.Query{
		Text:   query,
		Scopes: []memory.Scope{scope},
		K:      5,
		// VectorVetoScore / VectorVouchScore left 0 → the shipped defaults apply.
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var ids []string
	for _, r := range res.Recalled {
		ids = append(ids, r.ID)
	}
	t.Logf("recall (shipped defaults: veto=%.2f vouch=%.2f) -> %v", memory.DefaultVectorVetoScore, memory.DefaultSemanticThreshold, ids)
	for _, r := range res.Recalled {
		if r.ID == "go" {
			t.Fatalf("recall leaked the off-topic Go/GOPRIVATE fact for %q under shipped defaults, got %v", query, ids)
		}
	}
	if len(ids) == 0 {
		t.Fatalf("recall returned nothing — the on-topic fact should survive, got %v", ids)
	}

	// Cleanup the test collection.
	for _, r := range recs {
		_ = st.Delete(ctx, r.ID)
	}
}
