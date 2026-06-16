package market

import "testing"

func TestCalculateRollingPercentile(t *testing.T) {
	// Last value is the maximum in the window -> 100th percentile.
	series := []float64{1, 2, 3, 4, 5}
	p, ok := calculateRollingPercentile(series, 5)
	if !ok || p != 100.0 {
		t.Fatalf("expected percentile 100, got %v (ok=%v)", p, ok)
	}

	// Last value is the minimum -> 20th percentile (1/5).
	series = []float64{5, 4, 3, 2, 1}
	p, ok = calculateRollingPercentile(series, 5)
	if !ok || p != 20.0 {
		t.Fatalf("expected percentile 20, got %v (ok=%v)", p, ok)
	}

	// Window larger than series -> false.
	_, ok = calculateRollingPercentile(series, 6)
	if ok {
		t.Fatal("expected false when window > series length")
	}
}

func TestCalculateZScore(t *testing.T) {
	// Series with std dev = 0 -> false.
	_, ok := calculateZScore([]float64{1, 1, 1, 1}, 4)
	if ok {
		t.Fatal("expected false for zero variance series")
	}

	// Window too small -> false.
	_, ok = calculateZScore([]float64{1, 2}, 1)
	if ok {
		t.Fatal("expected false for window <= 1")
	}

	// Series 0..4: mean=2, variance=2, std=sqrt(2). Last=4 -> z=(4-2)/sqrt(2)=sqrt(2).
	series := []float64{0, 1, 2, 3, 4}
	z, ok := calculateZScore(series, 5)
	if !ok {
		t.Fatal("expected ok")
	}
	want := 1.4142135623730951
	if z < want-1e-9 || z > want+1e-9 {
		t.Fatalf("expected z-score %v, got %v", want, z)
	}

	// Last value below mean gives negative z-score.
	// Series 0..4 with last=0: z=(0-2)/sqrt(2)=-sqrt(2).
	series = []float64{4, 3, 2, 1, 0}
	z, ok = calculateZScore(series, 5)
	if !ok {
		t.Fatal("expected ok")
	}
	want = -1.4142135623730951
	if z < want-1e-9 || z > want+1e-9 {
		t.Fatalf("expected z-score %v, got %v", want, z)
	}
}
