package chroma

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// lexEmbedder is a deterministic, dependency-free embedder used only in tests: it
// feature-hashes tokens into a fixed-dim L2-normalized vector. It is purely
// lexical (no real semantics) but exercises the full Chroma plumbing — embedding,
// upsert, distance ranking, scope filtering — against a live server.
type lexEmbedder struct{ dim int }

func (e lexEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, e.dim)
		for _, tok := range strings.Fields(strings.ToLower(t)) {
			h := fnv.New32a()
			_, _ = h.Write([]byte(tok))
			v[h.Sum32()%uint32(e.dim)]++
		}
		var n float64
		for _, x := range v {
			n += float64(x) * float64(x)
		}
		if n > 0 {
			n = math.Sqrt(n)
			for j := range v {
				v[j] = float32(float64(v[j]) / n)
			}
		}
		out[i] = v
	}
	return out, nil
}

// TestStoreIntegration runs against a live Chroma server when CHROMA_TEST_URL is
// set (e.g. http://127.0.0.1:8001). It verifies the decorator's index sync, scope
// isolation at query time, capture exclusion, hybrid recall and delete.
func TestStoreIntegration(t *testing.T) {
	url := os.Getenv("CHROMA_TEST_URL")
	if url == "" {
		t.Skip("set CHROMA_TEST_URL to run the live Chroma integration test")
	}
	ctx := context.Background()
	client := NewClient(url)
	if err := client.Heartbeat(ctx); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	base := filestore.New(t.TempDir())
	coll := fmt.Sprintf("test_mem_%d", time.Now().UnixNano())
	st, err := NewStore(ctx, base, client, lexEmbedder{dim: 128}, coll, WithErrorHandler(func(e error) {
		t.Errorf("unexpected index error: %v", e)
	}))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	mk := func(id string, scope memory.Scope, text string, cat memory.Category, src memory.Source) memory.Record {
		r := memory.New(scope, text, cat, src)
		r.ID = id
		return r
	}
	recs := []memory.Record{
		mk("a", "proj-a", "use just targets never raw npx tsc", memory.CategoryPreference, memory.SourceAgent),
		mk("b", "proj-a", "auth flow lives in internal auth package", memory.CategoryProject, memory.SourceAgent),
		mk("c", "proj-b", "kubernetes deployment uses helm charts", memory.CategoryFact, memory.SourceAgent),
		mk("cap", "proj-a", "user ran just to build the project", memory.CategoryFact, memory.SourceCapture),
	}
	for _, r := range recs {
		if err := st.Put(ctx, r); err != nil {
			t.Fatalf("put %s: %v", r.ID, err)
		}
	}

	// Search within proj-a for a query lexically close to 'a'.
	hits, err := st.Search(ctx, "run build with npx and just", []memory.Scope{"proj-a"}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].ID != "a" {
		t.Fatalf("expected 'a' as top hit, got %+v", hits)
	}
	for _, h := range hits {
		if h.ID == "c" {
			t.Fatal("scope filter failed: proj-b record leaked into proj-a search")
		}
		if h.ID == "cap" {
			t.Fatal("captures must not be indexed/searchable")
		}
	}

	// Hybrid recall through the core (Store implements Searcher).
	res, err := memory.Recall(ctx, st, memory.Query{Text: "npx just build", Scopes: []memory.Scope{"proj-a"}, K: 3})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	found := false
	for _, r := range res.Recalled {
		if r.ID == "a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hybrid recall should surface 'a', got %+v", res.Recalled)
	}

	// Delete de-indexes.
	if err := st.Delete(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits2, err := st.Search(ctx, "run build with npx and just", []memory.Scope{"proj-a"}, 5)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	for _, h := range hits2 {
		if h.ID == "a" {
			t.Fatal("deleted record still in index")
		}
	}

	// Reindex from base should be a no-op error-wise.
	if err := st.Reindex(ctx); err != nil {
		t.Fatalf("reindex: %v", err)
	}
}
