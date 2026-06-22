package memory

import (
	"math"
	"testing"
)

// unit makes a 2-D unit vector at angle theta (radians); cosine of two such vectors
// is cos(thetaA-thetaB), giving full control over the pairwise distribution without
// an embedder.
func unit(theta float64) []float32 {
	return []float32{float32(math.Cos(theta)), float32(math.Sin(theta))}
}

func TestCalibrationSamplePercentile(t *testing.T) {
	// cosines 0.0,0.1,...,1.0 (11 values). Nearest-rank idx = int(p*10).
	s := CalibrationSample{cosines: []float64{0, .1, .2, .3, .4, .5, .6, .7, .8, .9, 1}}
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 0}, {0.5, 0.5}, {1, 1}, {0.99, 0.9}, {0.25, 0.2}, {-1, 0}, {2, 1},
	}
	for _, c := range cases {
		if got := s.Percentile(c.p); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := (CalibrationSample{}).Percentile(0.5); got != 0 {
		t.Errorf("empty Percentile = %v, want 0", got)
	}
}

func TestSampleCosineDistributionSortedAndCounts(t *testing.T) {
	// 4 distinct angles → 6 pairs, all measured (under the cap).
	vecs := [][]float32{unit(0), unit(0.3), unit(0.6), unit(1.2)}
	s := SampleCosineDistribution(vecs, 0)
	if s.Len() != 6 {
		t.Fatalf("Len = %d, want 6 (all pairs)", s.Len())
	}
	for i := 1; i < s.Len(); i++ {
		if s.cosines[i] < s.cosines[i-1] {
			t.Fatalf("not sorted ascending at %d: %v", i, s.cosines)
		}
	}
	// Max pair is the two closest angles (0 and 0.3 → cos 0.3 ≈ 0.955).
	if math.Abs(s.Max()-math.Cos(0.3)) > 1e-3 {
		t.Errorf("Max = %v, want ~%v", s.Max(), math.Cos(0.3))
	}
}

func TestSampleCosineDistributionDropsEmptyVectors(t *testing.T) {
	vecs := [][]float32{unit(0), nil, {}, unit(0.5)}
	s := SampleCosineDistribution(vecs, 0)
	if s.Len() != 1 { // only the one real pair survives
		t.Fatalf("Len = %d, want 1 (empties dropped)", s.Len())
	}
	if s.Len() < 1 || s.Min() < 0.8 { // cos(0.5) ≈ 0.878, no spurious 0 from the empties
		t.Errorf("unexpected cosine %v — an empty vector likely injected a 0", s.cosines)
	}
}

func TestSampleCosineDistributionDeterministicStride(t *testing.T) {
	// Enough vectors that the cap forces sampling; two runs must match exactly.
	n := 200
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = unit(float64(i) * 0.017)
	}
	a := SampleCosineDistribution(vecs, 500)
	b := SampleCosineDistribution(vecs, 500)
	if a.Len() != b.Len() {
		t.Fatalf("non-deterministic length: %d vs %d", a.Len(), b.Len())
	}
	if a.Len() == 0 || a.Len() > n*(n-1)/2 {
		t.Fatalf("unexpected sampled count %d", a.Len())
	}
	for i := range a.cosines {
		if a.cosines[i] != b.cosines[i] {
			t.Fatalf("non-deterministic sample at %d: %v vs %v", i, a.cosines[i], b.cosines[i])
		}
	}
}

func TestDeriveThresholdsPicksPercentiles(t *testing.T) {
	// Build a sample whose p25≈0.05 and p99≈0.42, mirroring the live all-MiniLM corpus,
	// and confirm the derived thresholds read those percentiles back.
	cosines := make([]float64, 0, 1000)
	for i := 0; i < 1000; i++ {
		cosines = append(cosines, float64(i)/1000*0.5) // 0.000..0.4995 linearly
	}
	s := CalibrationSample{cosines: cosines}
	res := DeriveThresholds(s, CalibrationOptions{}) // defaults: related p99, veto p25
	if !res.OK {
		t.Fatal("expected OK for a 1000-pair sample")
	}
	if got, want := res.Thresholds.CosineThreshold, s.Percentile(DefaultRelatedPercentile); math.Abs(got-want) > 1e-9 {
		t.Errorf("CosineThreshold = %v, want p99 %v", got, want)
	}
	if got, want := res.Thresholds.VectorVeto, s.Percentile(DefaultVetoPercentile); math.Abs(got-want) > 1e-9 {
		t.Errorf("VectorVeto = %v, want p25 %v", got, want)
	}
	// Ordering invariant: veto strictly below the related line.
	if res.Thresholds.VectorVeto >= res.Thresholds.CosineThreshold {
		t.Errorf("veto %v must be < cosThresh %v", res.Thresholds.VectorVeto, res.Thresholds.CosineThreshold)
	}
}

func TestDeriveThresholdsThinSampleFallsBack(t *testing.T) {
	s := CalibrationSample{cosines: make([]float64, minCalibrationPairs-1)}
	for i := range s.cosines {
		s.cosines[i] = 0.4
	}
	res := DeriveThresholds(s, CalibrationOptions{})
	if res.OK {
		t.Errorf("a sub-threshold sample must return OK=false so the caller keeps defaults")
	}
	if res.Thresholds != (CalibrationThresholds{}) {
		t.Errorf("thin sample should yield zero thresholds, got %+v", res.Thresholds)
	}
}

func TestDeriveThresholdsClampsNegativeVeto(t *testing.T) {
	// A distribution with a negative low tail (anti-correlated pairs) must clamp the
	// veto to 0, never negative.
	cosines := make([]float64, 0, 400)
	for i := 0; i < 400; i++ {
		cosines = append(cosines, -0.2+float64(i)/400*0.8) // -0.2 .. 0.6
	}
	res := DeriveThresholds(CalibrationSample{cosines: cosines}, CalibrationOptions{VetoPercentile: 0.05})
	if !res.OK {
		t.Fatal("expected OK")
	}
	if res.Thresholds.VectorVeto < 0 {
		t.Errorf("veto must clamp to >= 0, got %v", res.Thresholds.VectorVeto)
	}
}

func TestDeriveThresholdsFlatDistributionKeepsOrdering(t *testing.T) {
	// A perfectly flat distribution (every pair identical) would make veto==related;
	// the guard must still produce veto < related.
	cosines := make([]float64, 200)
	for i := range cosines {
		cosines[i] = 0.3
	}
	res := DeriveThresholds(CalibrationSample{cosines: cosines}, CalibrationOptions{})
	if !res.OK {
		t.Fatal("expected OK")
	}
	if res.Thresholds.VectorVeto >= res.Thresholds.CosineThreshold {
		t.Errorf("flat distribution broke the ordering invariant: veto %v >= cos %v",
			res.Thresholds.VectorVeto, res.Thresholds.CosineThreshold)
	}
}

func TestCalibrateExcludesGithubScore(t *testing.T) {
	// End-to-end on synthetic vectors: a corpus with genuine related clusters yields a
	// related line above the off-topic-keyword survivor's ~0.36, so it cannot vouch.
	// Six tight clusters of paraphrase-like vectors plus spread-out noise.
	var vecs [][]float32
	for c := 0; c < 6; c++ {
		base := float64(c) * (math.Pi / 6)
		for k := 0; k < 8; k++ {
			vecs = append(vecs, unit(base+float64(k)*0.01)) // intra-cluster cosine ~1
		}
	}
	for i := 0; i < 40; i++ {
		vecs = append(vecs, unit(float64(i)*0.19)) // spread noise
	}
	res := Calibrate(vecs, CalibrationOptions{})
	if !res.OK {
		t.Fatalf("expected OK, sample had %d pairs", res.Sample.Len())
	}
	const githubScore = 0.36
	if res.Thresholds.CosineThreshold <= githubScore {
		t.Errorf("cosThresh %v must exceed the off-topic survivor score %v so it can't vouch",
			res.Thresholds.CosineThreshold, githubScore)
	}
	if res.Thresholds.VectorVeto >= githubScore {
		t.Errorf("veto %v must sit below the weakly-related band (%v) so it doesn't over-veto",
			res.Thresholds.VectorVeto, githubScore)
	}
}
