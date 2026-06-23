package memory

import (
	"math"
	"testing"
)

// TestProjectPCA2DSeparatesClusters builds two clusters separated along one high-dim axis
// and asserts the projection pulls them apart along a principal axis — the property the
// semantic graph layout depends on (similar vectors land near each other).
func TestProjectPCA2DSeparatesClusters(t *testing.T) {
	const dim = 8
	var vectors [][]float32
	clusterA := []int{} // indices of cluster A
	clusterB := []int{}
	for i := 0; i < 5; i++ {
		a := make([]float32, dim)
		a[0] = 5 + 0.01*float32(i) // dominant +axis
		a[1] = 0.02 * float32(i)
		clusterA = append(clusterA, len(vectors))
		vectors = append(vectors, a)
	}
	for i := 0; i < 5; i++ {
		b := make([]float32, dim)
		b[0] = -5 + 0.01*float32(i) // dominant -axis
		b[1] = 0.02 * float32(i)
		clusterB = append(clusterB, len(vectors))
		vectors = append(vectors, b)
	}

	pts := ProjectPCA2D(vectors)
	if len(pts) != len(vectors) {
		t.Fatalf("want %d points, got %d", len(vectors), len(pts))
	}

	// The two clusters must be linearly separable along X (PC1) with a clear gap.
	maxA, minA := -math.MaxFloat64, math.MaxFloat64
	for _, i := range clusterA {
		maxA, minA = math.Max(maxA, pts[i].X), math.Min(minA, pts[i].X)
	}
	maxB, minB := -math.MaxFloat64, math.MaxFloat64
	for _, i := range clusterB {
		maxB, minB = math.Max(maxB, pts[i].X), math.Min(minB, pts[i].X)
	}
	sepAB := minA-maxB >= 0.8 // A entirely right of B
	sepBA := minB-maxA >= 0.8 // or B entirely right of A
	if !sepAB && !sepBA {
		t.Fatalf("clusters not separated along X: A=[%.3f,%.3f] B=[%.3f,%.3f]", minA, maxA, minB, maxB)
	}
}

func TestProjectPCA2DNormalizedRange(t *testing.T) {
	vectors := [][]float32{
		{1, 2, 3}, {-4, 5, -6}, {7, -8, 9}, {0, 0, 1}, {2, 2, 2},
	}
	for _, p := range ProjectPCA2D(vectors) {
		if p.X < -1.0000001 || p.X > 1.0000001 || p.Y < -1.0000001 || p.Y > 1.0000001 {
			t.Fatalf("point out of [-1,1]: %+v", p)
		}
	}
}

func TestProjectPCA2DDeterministic(t *testing.T) {
	vectors := [][]float32{
		{0.1, 0.9, -0.3, 0.2}, {0.8, -0.1, 0.4, -0.5},
		{-0.6, 0.2, 0.7, 0.1}, {0.3, 0.3, -0.8, 0.9},
	}
	a := ProjectPCA2D(vectors)
	b := ProjectPCA2D(vectors)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestProjectPCA2DDegenerate(t *testing.T) {
	if ProjectPCA2D(nil) != nil {
		t.Fatal("nil input should return nil")
	}
	if got := ProjectPCA2D([][]float32{{1, 2, 3}}); len(got) != 1 || got[0] != (Point2D{}) {
		t.Fatalf("single vector should sit at origin, got %+v", got)
	}
	// All-identical vectors have zero variance → all collapse to the origin, no NaNs.
	same := [][]float32{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}}
	for _, p := range ProjectPCA2D(same) {
		if p != (Point2D{}) || math.IsNaN(p.X) || math.IsNaN(p.Y) {
			t.Fatalf("identical vectors should collapse to origin without NaN, got %+v", p)
		}
	}
	// Zero-dimensional vectors → origins, no panic.
	for _, p := range ProjectPCA2D([][]float32{{}, {}}) {
		if p != (Point2D{}) {
			t.Fatalf("empty vectors should sit at origin, got %+v", p)
		}
	}
}
