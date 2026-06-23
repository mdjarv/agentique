package memory

import "math"

// Point2D is a 2D projection of a high-dimensional vector — e.g. an embedding laid out
// for a graph view, where spatial proximity reflects semantic similarity.
type Point2D struct {
	X float64
	Y float64
}

// projectIters bounds the power-iteration loop per component. all-MiniLM-style embeddings
// converge well under this; the loop also early-exits on convergence.
const projectIters = 128

// projectEps is the convergence threshold: when a power-iteration step moves the unit
// eigenvector estimate less than this, we stop.
const projectEps = 1e-7

// ProjectPCA2D projects each input vector onto the top-2 principal components of the set
// and normalizes the result into the [-1,1] square. It is the deterministic, dependency-free
// layout primitive behind the brain graph's "semantic" mode: similar vectors land near each
// other, so embedding clusters show up as spatial clusters.
//
// Determinism (so the layout is stable across reloads and unit-testable): a fixed,
// non-degenerate seed vector drives the power iteration, and each component's sign is
// canonicalized (its largest-magnitude coordinate is made positive) to remove PCA's inherent
// sign ambiguity. The second component is kept orthogonal to the first via Gram-Schmidt at
// every step.
//
// Degenerate inputs return safely: no vectors → nil; a single vector, or zero-dimensional
// vectors → all points at the origin. Vectors shorter than the first are zero-padded
// implicitly (treated as 0 in the missing dims) so a ragged set never panics.
func ProjectPCA2D(vectors [][]float32) []Point2D {
	n := len(vectors)
	if n == 0 {
		return nil
	}
	out := make([]Point2D, n)
	d := 0
	for _, v := range vectors {
		if len(v) > d {
			d = len(v)
		}
	}
	if d == 0 || n == 1 {
		return out // all at origin — nothing to spread
	}

	// Mean-center the columns (PCA operates on the centered matrix).
	x := make([][]float64, n)
	mean := make([]float64, d)
	for i, v := range vectors {
		row := make([]float64, d)
		for j := 0; j < d && j < len(v); j++ {
			row[j] = float64(v[j])
			mean[j] += row[j]
		}
		x[i] = row
	}
	for j := range mean {
		mean[j] /= float64(n)
	}
	for i := range x {
		for j := range x[i] {
			x[i][j] -= mean[j]
		}
	}

	pc1 := principalComponent(x, d, nil)
	pc2 := principalComponent(x, d, pc1)

	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range x {
		xs[i] = dot(x[i], pc1)
		ys[i] = dot(x[i], pc2)
	}
	normalizeInPlace(xs)
	normalizeInPlace(ys)
	for i := range out {
		out[i] = Point2D{X: xs[i], Y: ys[i]}
	}
	return out
}

// principalComponent returns the leading unit eigenvector of the covariance X^T X via
// power iteration, computed implicitly as v' = X^T (X v) so each step is O(n·d) rather than
// forming the d×d covariance. When orthoTo is non-nil the estimate is kept orthogonal to it
// at every step (Gram-Schmidt), yielding the next component. The sign is canonicalized for
// determinism.
func principalComponent(x [][]float64, d int, orthoTo []float64) []float64 {
	v := seedVector(d)
	if orthoTo != nil {
		orthogonalize(v, orthoTo)
		normalizeVec(v)
	}
	n := len(x)
	u := make([]float64, n)
	next := make([]float64, d)
	for iter := 0; iter < projectIters; iter++ {
		// u = X v
		for i := range x {
			u[i] = dot(x[i], v)
		}
		// next = X^T u
		for j := range next {
			next[j] = 0
		}
		for i := range x {
			ui := u[i]
			if ui == 0 {
				continue
			}
			row := x[i]
			for j := range row {
				next[j] += row[j] * ui
			}
		}
		if orthoTo != nil {
			orthogonalize(next, orthoTo)
		}
		if normalizeVec(next) == 0 {
			break // collapsed (e.g. all vectors identical along the remaining axis)
		}
		// Convergence: how far did the unit estimate move this step?
		delta := 0.0
		for j := range v {
			diff := next[j] - v[j]
			delta += diff * diff
		}
		copy(v, next)
		if delta < projectEps*projectEps {
			break
		}
	}
	canonicalizeSign(v)
	return v
}

// seedVector is a fixed, non-degenerate unit seed for power iteration. A constant vector
// would be a poor seed (often near-orthogonal to PC1); sin(j+1) varies across coordinates
// while staying fully deterministic.
func seedVector(d int) []float64 {
	v := make([]float64, d)
	for j := range v {
		v[j] = math.Sin(float64(j) + 1)
	}
	normalizeVec(v)
	return v
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// orthogonalize removes the component of v along the unit vector u (Gram-Schmidt).
func orthogonalize(v, u []float64) {
	proj := dot(v, u)
	for j := range v {
		v[j] -= proj * u[j]
	}
}

// normalizeVec scales v to unit length in place and returns the original norm (0 if v was
// the zero vector, in which case v is left unchanged).
func normalizeVec(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		return 0
	}
	for j := range v {
		v[j] /= norm
	}
	return norm
}

// canonicalizeSign flips v so its largest-magnitude coordinate is positive, removing PCA's
// sign ambiguity so the layout is reproducible.
func canonicalizeSign(v []float64) {
	maxAbs, sign := 0.0, 1.0
	for _, x := range v {
		if a := math.Abs(x); a > maxAbs {
			maxAbs, sign = a, math.Copysign(1, x)
		}
	}
	if sign < 0 {
		for j := range v {
			v[j] = -v[j]
		}
	}
}

// normalizeInPlace rescales a coordinate axis into [-1,1] by its max absolute value. A
// degenerate axis (all equal) collapses to 0.
func normalizeInPlace(a []float64) {
	maxAbs := 0.0
	for _, x := range a {
		if v := math.Abs(x); v > maxAbs {
			maxAbs = v
		}
	}
	if maxAbs == 0 {
		return
	}
	for i := range a {
		a[i] /= maxAbs
	}
}
