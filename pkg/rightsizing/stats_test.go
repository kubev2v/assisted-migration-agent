package rightsizing

import "testing"

func TestComputeStats_Empty(t *testing.T) {
	s := ComputeStats(nil)
	if s.SampleCount != 0 {
		t.Errorf("SampleCount: got %d, want 0", s.SampleCount)
	}
	if s.Average != 0 || s.P95 != 0 || s.P99 != 0 || s.Max != 0 || s.Latest != 0 {
		t.Errorf("expected all-zero MetricStats for empty input, got %+v", s)
	}
}

func TestComputeStats_SingleValue(t *testing.T) {
	s := ComputeStats([]int64{500})
	if s.SampleCount != 1 || s.Average != 500 || s.P95 != 500 || s.Max != 500 || s.Latest != 500 {
		t.Errorf("unexpected stats for single value: %+v", s)
	}
}

func TestComputeStats_LatestDistinctFromMax(t *testing.T) {
	// Descending input: latest (100) must differ from max (1000).
	values := []int64{1000, 900, 800, 700, 600, 500, 400, 300, 200, 100}
	s := ComputeStats(values)
	if s.Latest != 100 {
		t.Errorf("Latest: got %f, want 100", s.Latest)
	}
	if s.Max != 1000 {
		t.Errorf("Max: got %f, want 1000", s.Max)
	}
}

func TestComputeStats_DoesNotMutateInput(t *testing.T) {
	input := []int64{3, 1, 2}
	ComputeStats(input)
	if input[0] != 3 || input[1] != 1 || input[2] != 2 {
		t.Errorf("ComputeStats modified the input slice: got %v", input)
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	sorted := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		p    float64
		want float64
	}{
		{0.00, 1},  // max(-1, 0)=0 → sorted[0]=1
		{0.50, 5},  // ceil(5)-1=4 → sorted[4]=5
		{0.95, 10}, // ceil(9.5)-1=9 → sorted[9]=10
		{1.00, 10}, // ceil(10)-1=9 → sorted[9]=10
	}
	for _, tt := range tests {
		got := Percentile(sorted, tt.p)
		if got != tt.want {
			t.Errorf("Percentile(%v) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestPercentile_Empty(t *testing.T) {
	if got := Percentile(nil, 0.95); got != 0 {
		t.Errorf("Percentile(nil, 0.95) = %v, want 0", got)
	}
}
