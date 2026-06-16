// Package embedhttp implements memory.Embedder against any OpenAI-compatible
// /v1/embeddings endpoint (OpenAI, Ollama, LM Studio, vLLM, …). It uses only the
// standard library so it adds no dependencies and lifts alongside the core memory
// package. The host application chooses the endpoint and model; this keeps the
// (heavier, deployment-specific) embedding concern out of the core.
package embedhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Embedder calls an OpenAI-compatible embeddings endpoint.
type Embedder struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

// Option configures an Embedder.
type Option func(*Embedder)

// WithAPIKey sets a bearer token sent as Authorization.
func WithAPIKey(k string) Option { return func(e *Embedder) { e.apiKey = k } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(e *Embedder) { e.http = h } }

// New returns an Embedder. endpoint is the full embeddings URL (e.g.
// http://localhost:11434/v1/embeddings); model is the embedding model name.
func New(endpoint, model string, opts ...Option) *Embedder {
	e := &Embedder{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one vector per input text, preserving input order.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedhttp: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out embedResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("embedhttp: decode response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embedhttp: got %d embeddings for %d inputs", len(out.Data), len(texts))
	}
	// Order by the response index so vectors align with inputs regardless of
	// server ordering.
	sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Index < out.Data[j].Index })
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
