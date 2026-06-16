package embedhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedOrdersByIndex(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req embedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		// Return out of order to verify ordering by index.
		w.Write([]byte(`{"data":[{"index":1,"embedding":[0.3,0.4]},{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer srv.Close()

	e := New(srv.URL, "test-model", WithAPIKey("secret"))
	vecs, err := e.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][0] != 0.3 {
		t.Fatalf("vectors not aligned to input order: %+v", vecs)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header=%q", gotAuth)
	}
	if gotModel != "test-model" {
		t.Fatalf("model=%q", gotModel)
	}
}

func TestEmbedEmptyNoOp(t *testing.T) {
	e := New("http://unused", "m")
	vecs, err := e.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Fatalf("empty input should no-op, got %v %v", vecs, err)
	}
}

func TestEmbedCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "m").Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("expected error on count mismatch")
	}
}

func TestEmbedErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "m").Embed(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected error on 401")
	}
}
