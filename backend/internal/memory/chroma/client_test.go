package chroma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func testServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*httptest.Server, *[]map[string]any, *[]string) {
	var bodies []map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		bodies = append(bodies, body)
		paths = append(paths, r.Method+" "+r.URL.Path)
		h(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies, &paths
}

func TestClientHeartbeat(t *testing.T) {
	srv, _, paths := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Write([]byte(`{"nanosecond heartbeat":1}`))
	})
	if err := NewClient(srv.URL).Heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if (*paths)[0] != "GET /api/v2/heartbeat" {
		t.Fatalf("path=%q", (*paths)[0])
	}
}

func TestClientGetOrCreateCollection(t *testing.T) {
	srv, bodies, paths := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Write([]byte(`{"id":"coll-123","name":"mem"}`))
	})
	id, err := NewClient(srv.URL).GetOrCreateCollection(context.Background(), "mem")
	if err != nil {
		t.Fatal(err)
	}
	if id != "coll-123" {
		t.Fatalf("id=%q", id)
	}
	if !strings.HasSuffix((*paths)[0], "/collections") {
		t.Fatalf("path=%q", (*paths)[0])
	}
	b := (*bodies)[0]
	if b["name"] != "mem" || b["get_or_create"] != true {
		t.Fatalf("body=%+v", b)
	}
}

func TestClientUpsertSendsEmbeddings(t *testing.T) {
	srv, bodies, paths := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Write([]byte(`{}`))
	})
	err := NewClient(srv.URL).Upsert(context.Background(), "cid",
		[]string{"a"}, [][]float32{{0.1, 0.2}}, []string{"doc a"}, []map[string]any{{"scope": "g"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix((*paths)[0], "/collections/cid/upsert") {
		t.Fatalf("path=%q", (*paths)[0])
	}
	b := (*bodies)[0]
	if _, ok := b["embeddings"]; !ok {
		t.Fatalf("upsert must send embeddings, body=%+v", b)
	}
	if _, ok := b["ids"]; !ok {
		t.Fatalf("upsert must send ids, body=%+v", b)
	}
}

func TestClientUpsertEmptyNoOp(t *testing.T) {
	srv, _, paths := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {})
	if err := NewClient(srv.URL).Upsert(context.Background(), "cid", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(*paths) != 0 {
		t.Fatalf("empty upsert should make no request, got %v", *paths)
	}
}

func TestClientQueryParsesHits(t *testing.T) {
	srv, bodies, _ := testServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		if _, ok := body["query_embeddings"]; !ok {
			http.Error(w, "missing query_embeddings", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"ids":[["a","b"]],"distances":[[0.1,0.6]]}`))
	})
	hits, err := NewClient(srv.URL).Query(context.Background(), "cid", []float32{0.1, 0.2}, 5, map[string]any{"scope": "g"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "a" || hits[0].Distance != 0.1 || hits[1].ID != "b" {
		t.Fatalf("hits=%+v", hits)
	}
	if _, ok := (*bodies)[0]["where"]; !ok {
		t.Fatalf("query should include where, body=%+v", (*bodies)[0])
	}
}

func TestClientQueryEmptyResult(t *testing.T) {
	srv, _, _ := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Write([]byte(`{"ids":[[]],"distances":[[]]}`))
	})
	hits, err := NewClient(srv.URL).Query(context.Background(), "cid", []float32{0.1}, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestClientErrorStatus(t *testing.T) {
	srv, _, _ := testServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	})
	if err := NewClient(srv.URL).Heartbeat(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestDistanceToScore(t *testing.T) {
	cases := []struct{ d, want float64 }{
		{0, 1}, {0.5, 0.5}, {1, 0}, {1.5, 0}, {2, 0},
	}
	for _, c := range cases {
		if got := distanceToScore(c.d); got != c.want {
			t.Errorf("distanceToScore(%v)=%v want %v", c.d, got, c.want)
		}
	}
}

func TestScopeWhere(t *testing.T) {
	if scopeWhere(nil) != nil {
		t.Fatal("empty scopes should yield nil where")
	}
	w := scopeWhere([]memory.Scope{"a", "b"})
	scope, ok := w["scope"].(map[string]any)
	if !ok {
		t.Fatalf("where=%+v", w)
	}
	in, ok := scope["$in"].([]string)
	if !ok || len(in) != 2 || in[0] != "a" || in[1] != "b" {
		t.Fatalf("$in=%+v", scope["$in"])
	}
}
