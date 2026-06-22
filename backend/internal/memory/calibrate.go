package memory

import "sort"

// Auto-calibration derives the model-specific semantic thresholds (the cosine
// "related" line that doubles as the link threshold and the recall vouch bar, and
// the vector veto floor) from a corpus's OWN pairwise cosine distribution, instead
// of hand-tuning a constant per embedding model. The motivation: cosine
// distributions are compressed and shaped differently per model (all-MiniLM packs
// "related" around 0.44 and "unrelated" around 0.1; another model spreads them
// wider), so a single constant calibrated on one model is wrong on the next — and
// even on the same model, wrong for a corpus whose breadth differs from the sample
// it was tuned on (DefaultVectorVetoScore was tuned on a 5-fact example and is
// over-aggressive on a broad 1500-fact brain). See docs/brain-semantic-recall.md
// (sequencing #5) and docs/tech-debt.md ("cosine threshold is model-specific").
//
// The shape of a real corpus's pairwise distribution is the lever: the vast
// majority of fact pairs are unrelated (different topics), so the bulk sits low and
// only a thin tail is genuinely related. Hence:
//
//   - the related line is a HIGH percentile (only the top fraction of random pairs
//     are this similar — that is what "related" means), and
//   - the veto floor is a LOW percentile (a candidate the embedder scores below the
//     corpus's own least-similar pairs is actively unrelated).
//
// This file is pure (stdlib + CosineSimilarity only) so it lifts with the core
// memory package; the embedding of the live corpus and the env opt-in live in the
// agentique brain layer.

const (
	// DefaultRelatedPercentile is the pairwise-cosine percentile mapped to the
	// "related" line (cosine link threshold + recall vouch bar). p99 = "only the top
	// 1% of random fact pairs are this similar". On a broad real corpus (all-MiniLM)
	// this lands ~0.42, reproducing the hand-measured "p99 ≈ 0.44" knee and staying
	// above the off-topic-keyword survivors (~0.36) that must not vouch.
	DefaultRelatedPercentile = 0.99

	// DefaultVetoPercentile is the pairwise-cosine percentile mapped to the vector
	// veto floor ("actively unrelated"). A genuinely low percentile: a candidate the
	// embedder scores below the corpus's own bottom-quartile pairs is more dissimilar
	// to the query than 75% of random pairs are to each other. Kept above the
	// negative/near-zero tail (which clamps to 0 in query-score space and would
	// disable the veto) so it still fires on clearly-orthogonal facts.
	DefaultVetoPercentile = 0.25

	// DefaultMaxCalibrationPairs bounds how many pairs the distribution is sampled
	// over so a large corpus (n² pairs) can't blow up calibration. 200k pairs gives
	// stable percentiles (verified against the live brain) while staying sub-second.
	DefaultMaxCalibrationPairs = 200_000

	// minCalibrationPairs is the floor below which the sampled distribution is too
	// thin for its percentiles to be meaningful, so DeriveThresholds declines (the
	// caller keeps its hand-set defaults). 100 pairs ≈ a ≥15-fact corpus.
	minCalibrationPairs = 100
)

// CalibrationOptions tune the percentile→threshold mapping and the sampling cap.
// The zero value uses the defaults above.
type CalibrationOptions struct {
	// RelatedPercentile maps to the cosine "related" line; 0 uses DefaultRelatedPercentile.
	RelatedPercentile float64
	// VetoPercentile maps to the vector veto floor; 0 uses DefaultVetoPercentile.
	VetoPercentile float64
	// MaxPairs caps the sampled pair count; 0 uses DefaultMaxCalibrationPairs.
	MaxPairs int
}

func (o CalibrationOptions) withDefaults() CalibrationOptions {
	if o.RelatedPercentile <= 0 {
		o.RelatedPercentile = DefaultRelatedPercentile
	}
	if o.VetoPercentile <= 0 {
		o.VetoPercentile = DefaultVetoPercentile
	}
	if o.MaxPairs <= 0 {
		o.MaxPairs = DefaultMaxCalibrationPairs
	}
	return o
}

// CalibrationThresholds is the derived, model-specific threshold set. CosineThreshold
// feeds WithCosineThreshold / Query.VectorVouchScore (the "related" line); VectorVeto
// feeds Query.VectorVetoScore (the "actively unrelated" floor).
type CalibrationThresholds struct {
	CosineThreshold float64
	VectorVeto      float64
}

// CalibrationSample is a sorted (ascending) sample of pairwise cosine similarities
// drawn from a corpus's own embeddings — the empirical basis for DeriveThresholds.
type CalibrationSample struct {
	cosines []float64 // ascending
}

// Len is the number of sampled pairs.
func (s CalibrationSample) Len() int { return len(s.cosines) }

// Percentile returns the p-th percentile (p in [0,1]) by nearest-rank. Returns 0 for
// an empty sample.
func (s CalibrationSample) Percentile(p float64) float64 {
	if len(s.cosines) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	idx := int(p * float64(len(s.cosines)-1))
	return s.cosines[idx]
}

// Min returns the smallest sampled cosine (0 if empty).
func (s CalibrationSample) Min() float64 {
	if len(s.cosines) == 0 {
		return 0
	}
	return s.cosines[0]
}

// Max returns the largest sampled cosine (0 if empty).
func (s CalibrationSample) Max() float64 {
	if len(s.cosines) == 0 {
		return 0
	}
	return s.cosines[len(s.cosines)-1]
}

// Mean returns the average sampled cosine (0 if empty).
func (s CalibrationSample) Mean() float64 {
	if len(s.cosines) == 0 {
		return 0
	}
	var sum float64
	for _, c := range s.cosines {
		sum += c
	}
	return sum / float64(len(s.cosines))
}

// CalibrationResult bundles the derived thresholds, the distribution they came from
// (for logging/inspection), and whether the sample was rich enough to trust. When
// OK is false the caller keeps its existing (hand-set) thresholds.
type CalibrationResult struct {
	Thresholds CalibrationThresholds
	Sample     CalibrationSample
	OK         bool
}

// SampleCosineDistribution computes the pairwise cosine similarities of vectors,
// sampling deterministically with a fixed stride when the all-pairs count exceeds
// maxPairs (0 = DefaultMaxCalibrationPairs). Empty/zero-length vectors are dropped
// first so they don't inject spurious zeros. The returned sample is sorted ascending.
//
// Deterministic (no RNG): the stride is a function of (n, maxPairs) and the pair
// enumeration is fixed-order, so repeated runs over the same corpus yield the same
// distribution — important for reproducible calibration and tests.
func SampleCosineDistribution(vectors [][]float32, maxPairs int) CalibrationSample {
	if maxPairs <= 0 {
		maxPairs = DefaultMaxCalibrationPairs
	}
	// Drop empty vectors up front: CosineSimilarity returns 0 for them, which would
	// pollute the low tail and bias the veto floor.
	vecs := make([][]float32, 0, len(vectors))
	for _, v := range vectors {
		if len(v) > 0 {
			vecs = append(vecs, v)
		}
	}
	n := len(vecs)
	if n < 2 {
		return CalibrationSample{}
	}
	total := n * (n - 1) / 2
	step := 1
	if total > maxPairs {
		step = total / maxPairs
	}
	out := make([]float64, 0, min(total, maxPairs)+1)
	idx := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if idx%step == 0 {
				out = append(out, CosineSimilarity(vecs[i], vecs[j]))
			}
			idx++
		}
	}
	sort.Float64s(out)
	return CalibrationSample{cosines: out}
}

// DeriveThresholds picks thresholds from the sample's percentiles per opts. It
// returns OK=false (and zero thresholds) when the sample is too thin
// (< minCalibrationPairs) or degenerate (related line ≤ 0), so the caller falls back
// to its hand-set defaults. The veto is clamped into [0, CosineThreshold) so it can
// never meet or exceed the related line (which would veto related facts) and a
// negative low-percentile tail clamps to a harmless 0.
func DeriveThresholds(sample CalibrationSample, opts CalibrationOptions) CalibrationResult {
	opts = opts.withDefaults()
	res := CalibrationResult{Sample: sample}
	if sample.Len() < minCalibrationPairs {
		return res
	}
	related := clamp01(sample.Percentile(opts.RelatedPercentile))
	if related <= 0 {
		return res
	}
	veto := sample.Percentile(opts.VetoPercentile)
	if veto < 0 {
		veto = 0
	}
	if veto >= related {
		// VetoPercentile < RelatedPercentile normally keeps veto < related; guard the
		// degenerate case (flat distribution) so the ordering invariant always holds.
		veto = related / 3
	}
	res.Thresholds = CalibrationThresholds{CosineThreshold: related, VectorVeto: veto}
	res.OK = true
	return res
}

// Calibrate is the one-call entry point: sample the corpus's pairwise cosine
// distribution and derive thresholds from it. Pure — it never errors (the host layer
// owns embedding, which can fail).
func Calibrate(vectors [][]float32, opts CalibrationOptions) CalibrationResult {
	opts = opts.withDefaults()
	sample := SampleCosineDistribution(vectors, opts.MaxPairs)
	return DeriveThresholds(sample, opts)
}
