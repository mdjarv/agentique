// Package vectortest provides in-process fakes of the two HTTP services a semantic brain
// depends on — a Chroma v2 server and an OpenAI-compatible embeddings endpoint — so tests can
// drive the real chroma.Client and embedhttp.Embedder through a backend that goes down and
// comes back, without containers.
//
// Both fakes answer 503 to everything while down. That is not what a removed container looks
// like on the wire (a refused dial), but every caller treats the two the same, and a fake that
// keeps its port is one a test can bring back.
package vectortest

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Chroma is a fake Chroma v2 server: collections, upsert, delete, get and query, with a
// brute-force cosine search and the scope $in filter chroma.Store sends.
type Chroma struct {
	URL string

	mu      sync.Mutex
	down    bool
	colls   map[string]*collection // by id
	names   map[string]string      // name -> id
	upserts int                    // ids upserted, cumulative
}

type collection struct {
	docs  map[string]string
	vecs  map[string][]float32
	metas map[string]map[string]any
}

// NewChroma starts a fake Chroma server that stops with the test.
func NewChroma(t testing.TB) *Chroma {
	t.Helper()
	c := &Chroma{colls: map[string]*collection{}, names: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(srv.Close)
	c.URL = srv.URL
	return c
}

// SetDown makes every request fail (true) or be served (false). Stored vectors survive.
func (c *Chroma) SetDown(down bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down = down
}

// Upserted returns how many ids have been upserted across every collection.
func (c *Chroma) Upserted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.upserts
}

// Docs returns the named collection's id → document map (nil when it does not exist).
func (c *Chroma) Docs(name string) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.names[name]
	if !ok {
		return nil
	}
	out := make(map[string]string, len(c.colls[id].docs))
	for k, v := range c.colls[id].docs {
		out[k] = v
	}
	return out
}

func (c *Chroma) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.down {
		http.Error(w, "fake chroma is down", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	path := r.URL.Path
	switch {
	case path == "/api/v2/heartbeat":
		writeJSON(w, map[string]any{"nanosecond heartbeat": 1})
	case strings.HasSuffix(path, "/collections"):
		name, _ := body["name"].(string)
		id, ok := c.names[name]
		if !ok {
			id = "coll-" + name
			c.names[name] = id
			c.colls[id] = &collection{docs: map[string]string{}, vecs: map[string][]float32{}, metas: map[string]map[string]any{}}
		}
		writeJSON(w, map[string]any{"id": id, "name": name})
	default:
		c.serveCollection(w, path, body)
	}
}

func (c *Chroma) serveCollection(w http.ResponseWriter, path string, body map[string]any) {
	_, rest, ok := strings.Cut(path, "/collections/")
	if !ok {
		http.NotFound(w, nil)
		return
	}
	id, op, _ := strings.Cut(rest, "/")
	coll, ok := c.colls[id]
	if !ok {
		http.Error(w, "no such collection", http.StatusNotFound)
		return
	}
	switch op {
	case "upsert":
		ids := stringsOf(body["ids"])
		docs := stringsOf(body["documents"])
		embs, _ := body["embeddings"].([]any)
		metas, _ := body["metadatas"].([]any)
		for i, vid := range ids {
			if i < len(docs) {
				coll.docs[vid] = docs[i]
			}
			if i < len(embs) {
				coll.vecs[vid] = floats(embs[i])
			}
			if i < len(metas) {
				m, _ := metas[i].(map[string]any)
				coll.metas[vid] = m
			}
		}
		c.upserts += len(ids)
		writeJSON(w, map[string]any{})
	case "delete":
		for _, vid := range stringsOf(body["ids"]) {
			delete(coll.docs, vid)
			delete(coll.vecs, vid)
			delete(coll.metas, vid)
		}
		writeJSON(w, map[string]any{})
	case "get":
		include := stringsOf(body["include"])
		out := map[string]any{}
		var ids, docs []string
		var embs [][]float32
		for vid, doc := range coll.docs {
			ids = append(ids, vid)
			docs = append(docs, doc)
			embs = append(embs, coll.vecs[vid])
		}
		out["ids"] = ids
		for _, inc := range include {
			switch inc {
			case "documents":
				out["documents"] = docs
			case "embeddings":
				out["embeddings"] = embs
			}
		}
		writeJSON(w, out)
	case "query":
		qs, _ := body["query_embeddings"].([]any)
		if len(qs) == 0 {
			http.Error(w, "no query embedding", http.StatusBadRequest)
			return
		}
		q := floats(qs[0])
		n, _ := body["n_results"].(float64)
		allowed := scopeFilter(body["where"])
		type hit struct {
			id   string
			dist float64
		}
		var hits []hit
		for vid, v := range coll.vecs {
			if allowed != nil {
				scope, _ := coll.metas[vid]["scope"].(string)
				if !allowed[scope] {
					continue
				}
			}
			hits = append(hits, hit{vid, 1 - cosine(q, v)})
		}
		for i := 1; i < len(hits); i++ { // insertion sort: tiny corpora
			for j := i; j > 0 && hits[j].dist < hits[j-1].dist; j-- {
				hits[j], hits[j-1] = hits[j-1], hits[j]
			}
		}
		if int(n) > 0 && len(hits) > int(n) {
			hits = hits[:int(n)]
		}
		ids := make([]string, len(hits))
		dists := make([]float64, len(hits))
		for i, h := range hits {
			ids[i], dists[i] = h.id, h.dist
		}
		writeJSON(w, map[string]any{"ids": [][]string{ids}, "distances": [][]float64{dists}})
	default:
		http.NotFound(w, nil)
	}
}

// Embedder is a fake OpenAI-compatible embeddings endpoint. A text's vector is a hashed bag of
// its lower-cased words, so texts sharing words score as similar — enough signal for recall
// to rank by, with no model.
type Embedder struct {
	URL string

	mu    sync.Mutex
	down  bool
	texts int
	max   int
}

// EmbedDim is the fake embedder's vector width.
const EmbedDim = 64

// NewEmbedder starts a fake embeddings endpoint that stops with the test. Its URL is the full
// embeddings URL, as embedhttp.New expects.
func NewEmbedder(t testing.TB) *Embedder {
	t.Helper()
	e := &Embedder{}
	srv := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/v1/embeddings"
	return e
}

// SetDown makes every request fail (true) or be served (false).
func (e *Embedder) SetDown(down bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.down = down
}

// Texts returns how many texts have been embedded, cumulative.
func (e *Embedder) Texts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.texts
}

// MaxBatch returns the largest number of inputs one request carried.
func (e *Embedder) MaxBatch() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.max
}

func (e *Embedder) serve(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		http.Error(w, "fake embedder is down", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e.texts += len(req.Input)
	e.max = max(e.max, len(req.Input))
	data := make([]map[string]any, len(req.Input))
	for i, text := range req.Input {
		data[i] = map[string]any{"index": i, "embedding": Vector(text)}
	}
	writeJSON(w, map[string]any{"data": data})
}

// Vector is the fake embedder's vector for text.
func Vector(text string) []float32 {
	v := make([]float32, EmbedDim)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		v[h.Sum32()%EmbedDim]++
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func stringsOf(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func floats(v any) []float32 {
	arr, _ := v.([]any)
	out := make([]float32, len(arr))
	for i, x := range arr {
		f, _ := x.(float64)
		out[i] = float32(f)
	}
	return out
}

func scopeFilter(where any) map[string]bool {
	w, _ := where.(map[string]any)
	scope, _ := w["scope"].(map[string]any)
	in, ok := scope["$in"]
	if !ok {
		return nil
	}
	allowed := map[string]bool{}
	for _, s := range stringsOf(in) {
		allowed[s] = true
	}
	return allowed
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
