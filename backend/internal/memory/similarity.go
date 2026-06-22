package memory

// DefaultSemanticThreshold is the cosine similarity at or above which two facts are
// linked when embeddings are available. It sits far higher than the lexical Jaccard
// thresholds because embedding cosine is compressed and dense — a measure-first sweep on
// a real corpus (quantized all-MiniLM-L6-v2) put the clean knee around 0.45–0.55 (p99 of
// all pairs ≈ 0.44). It is MODEL-SPECIFIC: recalibrate per embedding model. Tunable via
// WithCosineThreshold.
const DefaultSemanticThreshold = 0.45

// maxSemanticDegree caps how many embedding-similarity neighbours a fact keeps when
// clustering. Cosine is far denser than Jaccard and generic "hub" facts connect to
// everything; without a cap, label propagation chains the whole corpus into one mega-
// cluster (the measure-first finding). The cap mirrors RelinkScope's maxRelatedDegree.
const maxSemanticDegree = 8

// Similarity scores text relatedness between the records of a fixed slice, addressed by
// index. It combines lexical token-Jaccard with optional embedding cosine: when both
// records in a pair carry an embedding, the score is max(jaccard, cosine), so a pair
// that is similar by EITHER signal links — lexical-but-not-semantic ("go test" vs "go
// build") and semantic-but-not-lexical ("race detector" vs "concurrent safety") both
// surface. With no embeddings it is pure Jaccard, so keyword-only behaviour is unchanged.
//
// It is the single similarity primitive behind RelinkScope, DetectCommunities,
// CrossScopeGroups and interference, so swapping in embeddings uplifts all of them at
// once (RFC brain-cross-scope-areas, phase C).
type Similarity struct {
	toks      []map[string]struct{}
	embs      [][]float32 // nil = lexical only; otherwise per-index vector (nil entry = no vector)
	cosThresh float64
}

// simConfig is assembled from SimOptions.
type simConfig struct {
	embLookup func(id string) []float32
	cosThresh float64
}

// SimOption configures how a Similarity is built. The zero set yields pure-Jaccard
// similarity, so every existing caller is unaffected.
type SimOption func(*simConfig)

// WithEmbeddingLookup supplies per-record embedding vectors (by id) so similarity blends
// in cosine. A record the lookup has no vector for (returns nil/empty) falls back to
// Jaccard for its pairs — semantic mode degrades gracefully per record.
func WithEmbeddingLookup(lookup func(id string) []float32) SimOption {
	return func(c *simConfig) { c.embLookup = lookup }
}

// WithCosineThreshold overrides DefaultSemanticThreshold (calibrate per embedding model).
func WithCosineThreshold(t float64) SimOption {
	return func(c *simConfig) { c.cosThresh = t }
}

// newSimilarity builds the similarity over records, applying any options.
func newSimilarity(records []Record, opts ...SimOption) *Similarity {
	cfg := simConfig{cosThresh: DefaultSemanticThreshold}
	for _, o := range opts {
		o(&cfg)
	}
	s := &Similarity{toks: make([]map[string]struct{}, len(records)), cosThresh: cfg.cosThresh}
	for i, r := range records {
		s.toks[i] = tokenSet(r.Text)
	}
	if cfg.embLookup != nil {
		embs := make([][]float32, len(records))
		any := false
		for i, r := range records {
			if v := cfg.embLookup(r.ID); len(v) > 0 {
				embs[i] = v
				any = true
			}
		}
		if any {
			s.embs = embs
		}
	}
	return s
}

// Linked reports whether records i and j are similar enough to connect. A pair links if
// EITHER signal clears its own threshold — lexical Jaccard ≥ lexThresh OR embedding
// cosine ≥ the (separate, higher) cosine threshold — so lexical-only and semantic-only
// neighbours both connect. The two-threshold form is deliberate: a single threshold on a
// blended score would either drop weak-but-real lexical links or collapse the dense
// cosine graph (measure-first finding).
func (s *Similarity) Linked(i, j int, lexThresh float64) bool {
	if jaccardSets(s.toks[i], s.toks[j]) >= lexThresh {
		return true
	}
	return s.embs != nil && CosineSimilarity(s.embs[i], s.embs[j]) >= s.cosThresh
}

// Score returns max(jaccard, cosine) — a single strength used to rank a node's neighbours
// when capping degree, not for the link decision (that's Linked).
func (s *Similarity) Score(i, j int) float64 {
	lex := jaccardSets(s.toks[i], s.toks[j])
	if s.embs == nil {
		return lex
	}
	if cos := CosineSimilarity(s.embs[i], s.embs[j]); cos > lex {
		return cos
	}
	return lex
}

// semantic reports whether any embedding signal is active (the degree cap only matters
// when the denser cosine graph is in play).
func (s *Similarity) semantic() bool { return s.embs != nil }

// interference reports whether pair (i,j) sits in the "related but not a lexical
// duplicate" band (DetectInterference), and a representative similarity for ranking.
// Related uses the same two-threshold OR as Linked — jaccard ≥ lexLower OR cosine ≥
// cosThresh — so a semantic-only near-match (low Jaccard, high cosine) counts, which is
// exactly the pair an agent confuses. Duplicate-exclusion stays LEXICAL (jaccard ≥
// lexUpper): consolidation merges lexical dups, so a high-cosine / low-jaccard pair is a
// surviving semantic near-duplicate that SHOULD surface as interference, not be excluded
// as a "duplicate". Degrades to pure Jaccard with no embeddings (band unchanged).
func (s *Similarity) interference(i, j int, lexLower, lexUpper float64) (score float64, inBand bool) {
	lex := jaccardSets(s.toks[i], s.toks[j])
	if lex >= lexUpper {
		return lex, false // lexical duplicate — consolidation's job, not interference
	}
	score, related := lex, lex >= lexLower
	if s.embs != nil {
		cos := CosineSimilarity(s.embs[i], s.embs[j]) // negative cos can't trip either test below
		if cos > score {
			score = cos
		}
		if cos >= s.cosThresh {
			related = true
		}
	}
	return score, related
}
