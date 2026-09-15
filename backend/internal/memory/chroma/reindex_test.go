package chroma

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// cappedEmbedder refuses a request carrying more than max inputs, the way a
// real endpoint does: OpenAI caps a request at 2048 inputs, and
// text-embeddings-inference answers 429 "Model is overloaded" past its
// in-flight limit (512 by default).
type cappedEmbedder struct {
	max   int
	calls []int
}

func (e *cappedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, len(texts))
	if len(texts) > e.max {
		return nil, fmt.Errorf("status 429: %d inputs in one request", len(texts))
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i), 1}
	}
	return out, nil
}

// A corpus larger than one request's worth of inputs must still reindex whole:
// Reindex embeds and upserts in bounded batches rather than the corpus at once.
func TestReindexBatchesACorpusLargerThanOneRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	upserted := 0
	srv, _, _ := testServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/collections"):
			w.Write([]byte(`{"id":"cid","name":"mem"}`))
		case strings.HasSuffix(r.URL.Path, "/upsert"):
			ids, _ := body["ids"].([]any)
			embs, _ := body["embeddings"].([]any)
			if len(ids) != len(embs) {
				t.Errorf("upsert carried %d ids and %d embeddings", len(ids), len(embs))
			}
			upserted += len(ids)
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{}`))
		}
	})

	base := filestore.New(t.TempDir())
	const facts = 2*reindexBatch + 7
	for i := range facts {
		r := memory.New("proj", fmt.Sprintf("fact number %d", i), memory.CategoryFact, memory.SourceAgent)
		if err := base.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// A capture is never indexed, so it must not count toward what is upserted.
	if err := base.Put(ctx, memory.New("proj", "a capture", memory.CategoryFact, memory.SourceCapture)); err != nil {
		t.Fatal(err)
	}

	emb := &cappedEmbedder{max: reindexBatch}
	st, err := NewStore(ctx, base, NewClient(srv.URL), emb, "mem")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reindex(ctx); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if upserted != facts {
		t.Fatalf("upserted %d vectors, want %d", upserted, facts)
	}
	for _, n := range emb.calls {
		if n > reindexBatch {
			t.Fatalf("an embed request carried %d inputs, over the %d batch", n, reindexBatch)
		}
	}
}

// An embedder that answers with fewer vectors than it was asked for must fail
// the reindex, never upsert ids against embeddings they do not belong to.
func TestReindexRefusesAShortEmbedAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	srv, _, paths := testServer(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if strings.HasSuffix(r.URL.Path, "/collections") {
			w.Write([]byte(`{"id":"cid","name":"mem"}`))
			return
		}
		w.Write([]byte(`{}`))
	})
	base := filestore.New(t.TempDir())
	for i := range 3 {
		if err := base.Put(ctx, memory.New("proj", fmt.Sprintf("fact %d", i), memory.CategoryFact, memory.SourceAgent)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := NewStore(ctx, base, NewClient(srv.URL), shortEmbedder{}, "mem")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reindex(ctx); err == nil {
		t.Fatal("reindex succeeded on a short embed answer")
	}
	for _, p := range *paths {
		if strings.HasSuffix(p, "/upsert") {
			t.Fatalf("upserted despite a short embed answer: %v", *paths)
		}
	}
}

type shortEmbedder struct{}

func (shortEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)-1), nil
}
