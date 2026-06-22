// Package chroma provides a minimal ChromaDB client and a memory.Store decorator
// that adds semantic (vector) recall on top of a base Store. It uses only the
// standard library (net/http, encoding/json) so it adds no dependencies and lifts
// into a shared library alongside the core memory package.
//
// The decorator treats the base Store (e.g. a filestore) as the source of truth
// and Chroma as a derived, rebuildable index: durable writes never fail because
// the index is unavailable, and recall degrades to keyword ranking when Chroma is
// down (see memory.Recall). Embeddings are computed server-side by Chroma's
// default embedding function, so no Embedder is required.
package chroma

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTenant   = "default_tenant"
	defaultDatabase = "default_database"
)

// Client is a minimal ChromaDB v2 HTTP client.
type Client struct {
	baseURL  string
	tenant   string
	database string
	http     *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithTenant sets the Chroma tenant (default "default_tenant").
func WithTenant(t string) Option { return func(c *Client) { c.tenant = t } }

// WithDatabase sets the Chroma database (default "default_database").
func WithDatabase(d string) Option { return func(c *Client) { c.database = d } }

// WithHTTPClient overrides the underlying HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// NewClient returns a Client for a Chroma server at baseURL (e.g. http://localhost:8000).
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		tenant:   defaultTenant,
		database: defaultDatabase,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) dbPath(suffix string) string {
	return fmt.Sprintf("/api/v2/tenants/%s/databases/%s%s", c.tenant, c.database, suffix)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("chroma %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("chroma %s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

// Heartbeat verifies the server is reachable and healthy.
func (c *Client) Heartbeat(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v2/heartbeat", nil, nil)
}

// GetOrCreateCollection returns the collection ID for name, creating it (with a
// cosine HNSW space) if it does not exist.
func (c *Client) GetOrCreateCollection(ctx context.Context, name string) (string, error) {
	body := map[string]any{
		"name":          name,
		"get_or_create": true,
		"metadata":      map[string]any{"hnsw:space": "cosine"},
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, c.dbPath("/collections"), body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("chroma: collection %q returned no id", name)
	}
	return out.ID, nil
}

// Upsert inserts or replaces documents. Chroma 1.x does not embed server-side, so
// caller-computed embeddings are required (one per id, same order). Documents are
// stored alongside for readability/debugging in the Chroma UI.
func (c *Client) Upsert(ctx context.Context, collID string, ids []string, embeddings [][]float32, documents []string, metadatas []map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	body := map[string]any{
		"ids":        ids,
		"embeddings": embeddings,
		"documents":  documents,
		"metadatas":  metadatas,
	}
	return c.do(ctx, http.MethodPost, c.dbPath("/collections/"+collID+"/upsert"), body, nil)
}

// Delete removes documents by ID.
func (c *Client) Delete(ctx context.Context, collID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	body := map[string]any{"ids": ids}
	return c.do(ctx, http.MethodPost, c.dbPath("/collections/"+collID+"/delete"), body, nil)
}

// VectorRecord is one stored document and its embedding, returned by GetEmbeddings.
type VectorRecord struct {
	ID        string
	Document  string
	Embedding []float32
}

// GetEmbeddings fetches stored documents and their embeddings, returning all rows when ids is
// empty. It exists to warm an in-process embedding cache after a restart: Chroma already holds
// every fact's vector, so unchanged facts need never be re-embedded. Embeddings are NOT
// returned by default, so they are requested explicitly via include. Rows missing a document
// or embedding are still returned; the caller filters. At current corpus sizes (dozens–low
// thousands) a single unpaginated get is fine.
func (c *Client) GetEmbeddings(ctx context.Context, collID string, ids []string) ([]VectorRecord, error) {
	body := map[string]any{"include": []string{"embeddings", "documents"}}
	if len(ids) > 0 {
		body["ids"] = ids
	}
	var out struct {
		IDs        []string    `json:"ids"`
		Documents  []string    `json:"documents"`
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := c.do(ctx, http.MethodPost, c.dbPath("/collections/"+collID+"/get"), body, &out); err != nil {
		return nil, err
	}
	recs := make([]VectorRecord, 0, len(out.IDs))
	for i, id := range out.IDs {
		r := VectorRecord{ID: id}
		if i < len(out.Documents) {
			r.Document = out.Documents[i]
		}
		if i < len(out.Embeddings) {
			r.Embedding = out.Embeddings[i]
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// QueryHit is one semantic search result.
type QueryHit struct {
	ID       string
	Distance float64
}

// Query returns the nearest documents to a caller-computed query embedding,
// optionally filtered by a where clause (e.g. {"scope": {"$in": [...]}}).
func (c *Client) Query(ctx context.Context, collID string, embedding []float32, nResults int, where map[string]any) ([]QueryHit, error) {
	body := map[string]any{
		"query_embeddings": [][]float32{embedding},
		"n_results":        nResults,
		"include":          []string{"distances"},
	}
	if len(where) > 0 {
		body["where"] = where
	}
	var out struct {
		IDs       [][]string  `json:"ids"`
		Distances [][]float64 `json:"distances"`
	}
	if err := c.do(ctx, http.MethodPost, c.dbPath("/collections/"+collID+"/query"), body, &out); err != nil {
		return nil, err
	}
	if len(out.IDs) == 0 {
		return nil, nil
	}
	hits := make([]QueryHit, 0, len(out.IDs[0]))
	for i, id := range out.IDs[0] {
		h := QueryHit{ID: id}
		if len(out.Distances) > 0 && i < len(out.Distances[0]) {
			h.Distance = out.Distances[0][i]
		}
		hits = append(hits, h)
	}
	return hits, nil
}
