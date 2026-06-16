package memory

import (
	"context"
	"math"
)

// Embedder turns text into vectors for semantic similarity. It is optional: when
// no Embedder (and no vector-capable Store) is available, Recall falls back to
// keyword ranking only. Implementations live in the host application or in
// optional adapters so this package stays dependency-free.
type Embedder interface {
	// Embed returns one vector per input text, in order. All vectors share a
	// fixed dimension for a given Embedder.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// CosineSimilarity returns the cosine similarity of two equal-length vectors in
// [-1,1]. It returns 0 if either vector is empty, zero-magnitude, or the lengths
// differ — callers treat 0 as "no signal".
func CosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
