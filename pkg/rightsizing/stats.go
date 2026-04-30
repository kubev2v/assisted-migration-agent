package rightsizing

import (
	"math"
	"slices"
)

// MetricStats summarizes a set of int64 metric samples from vSphere QueryPerf.
// Units depend on the counter; see DesiredMetrics comments in vsphere.go.
type MetricStats struct {
	SampleCount int     `json:"sample_count"`
	Average     float64 `json:"average"`
	P95         float64 `json:"p95"`
	P99         float64 `json:"p99"`
	Max         float64 `json:"max"`
	Latest      float64 `json:"latest"`
}

// ComputeStats aggregates a slice of int64 samples into MetricStats.
// Latest is the last element of values (time-ordered). The input slice is not modified.
func ComputeStats(values []int64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}

	sorted := make([]int64, len(values))
	copy(sorted, values)
	slices.Sort(sorted)

	var sum int64
	for _, v := range values {
		sum += v
	}

	n := len(sorted)
	return MetricStats{
		SampleCount: n,
		Average:     float64(sum) / float64(n),
		P95:         Percentile(sorted, 0.95),
		P99:         Percentile(sorted, 0.99),
		Max:         float64(sorted[n-1]),
		Latest:      float64(values[len(values)-1]),
	}
}

// Percentile returns the p-th percentile (0.0–1.0) of a sorted int64 slice
// using the nearest-rank method: idx = ceil(p * n) - 1.
// p must be in (0.0, 1.0]; p=0.0 is clamped to sorted[0].
func Percentile(sorted []int64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := max(int(math.Ceil(p*float64(n)))-1, 0)
	if idx >= n {
		idx = n - 1
	}
	return float64(sorted[idx])
}
