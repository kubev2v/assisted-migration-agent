package services

import (
	"math"
	"testing"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestComputeForecastStats_EmptyRuns(t *testing.T) {
	stats := computeForecastStats("test-pair", nil)
	if stats.SampleCount != 0 {
		t.Errorf("expected 0 samples, got %d", stats.SampleCount)
	}
	if stats.PairName != "test-pair" {
		t.Errorf("expected pair name 'test-pair', got %q", stats.PairName)
	}
}

func TestComputeForecastStats_AllErrors(t *testing.T) {
	runs := []models.BenchmarkRun{
		{PairName: "pair1", Error: "disk copy failed"},
		{PairName: "pair1", Error: "timeout"},
	}
	stats := computeForecastStats("pair1", runs)
	if stats.SampleCount != 0 {
		t.Errorf("expected 0 samples, got %d", stats.SampleCount)
	}
}

func TestComputeForecastStats_SingleRun(t *testing.T) {
	runs := []models.BenchmarkRun{
		{PairName: "pair1", ThroughputMBps: 500.0, DiskSizeGB: 10},
	}
	stats := computeForecastStats("pair1", runs)
	if stats.SampleCount != 1 {
		t.Errorf("expected 1 sample, got %d", stats.SampleCount)
	}
	if stats.MeanMBps != 500.0 {
		t.Errorf("expected mean 500.0, got %f", stats.MeanMBps)
	}
	if stats.MedianMBps != 500.0 {
		t.Errorf("expected median 500.0, got %f", stats.MedianMBps)
	}
	if stats.StdDevMBps != 0 {
		t.Errorf("expected stddev 0 for single sample, got %f", stats.StdDevMBps)
	}
	// CI should be equal to mean for single sample
	if stats.CI95Lower != stats.MeanMBps || stats.CI95Upper != stats.MeanMBps {
		t.Errorf("expected CI = mean for single sample, got [%f, %f]", stats.CI95Lower, stats.CI95Upper)
	}
}

func TestComputeForecastStats_MultipleRuns(t *testing.T) {
	runs := []models.BenchmarkRun{
		{PairName: "pair1", ThroughputMBps: 100.0},
		{PairName: "pair1", ThroughputMBps: 200.0},
		{PairName: "pair1", ThroughputMBps: 300.0},
		{PairName: "pair1", ThroughputMBps: 400.0},
		{PairName: "pair1", ThroughputMBps: 500.0},
	}
	stats := computeForecastStats("pair1", runs)

	if stats.SampleCount != 5 {
		t.Errorf("expected 5 samples, got %d", stats.SampleCount)
	}
	if stats.MinMBps != 100.0 {
		t.Errorf("expected min 100.0, got %f", stats.MinMBps)
	}
	if stats.MaxMBps != 500.0 {
		t.Errorf("expected max 500.0, got %f", stats.MaxMBps)
	}
	if stats.MeanMBps != 300.0 {
		t.Errorf("expected mean 300.0, got %f", stats.MeanMBps)
	}
	if stats.MedianMBps != 300.0 {
		t.Errorf("expected median 300.0, got %f", stats.MedianMBps)
	}

	// StdDev for [100, 200, 300, 400, 500]: variance = sum((x-300)^2)/(n-1)
	// = (40000+10000+0+10000+40000)/4 = 25000, stddev = sqrt(25000) ≈ 158.11
	expectedStdDev := math.Sqrt(100000.0 / 4.0)
	if math.Abs(stats.StdDevMBps-expectedStdDev) > 0.01 {
		t.Errorf("expected stddev ~%.2f, got %f", expectedStdDev, stats.StdDevMBps)
	}

	// CI95 should be narrower than the range
	if stats.CI95Lower >= stats.CI95Upper {
		t.Error("CI95 lower should be less than upper")
	}
	if stats.CI95Lower < 0 {
		t.Error("CI95 lower should not be negative")
	}
}

func TestComputeForecastStats_FiltersErrors(t *testing.T) {
	runs := []models.BenchmarkRun{
		{PairName: "pair1", ThroughputMBps: 100.0},
		{PairName: "pair1", ThroughputMBps: 0, Error: "failed"},
		{PairName: "pair1", ThroughputMBps: 200.0},
		{PairName: "pair1", ThroughputMBps: 300.0},
	}
	stats := computeForecastStats("pair1", runs)
	if stats.SampleCount != 3 {
		t.Errorf("expected 3 samples (error filtered), got %d", stats.SampleCount)
	}
	if stats.MeanMBps != 200.0 {
		t.Errorf("expected mean 200.0, got %f", stats.MeanMBps)
	}
}

func TestComputeForecastStats_Estimates(t *testing.T) {
	runs := []models.BenchmarkRun{
		{PairName: "pair1", ThroughputMBps: 1024.0}, // 1 GB/s = 1TB in ~17 min
	}
	stats := computeForecastStats("pair1", runs)

	// 1TB = 1048576 MB, at 1024 MB/s = 1024 seconds = ~17 min
	expectedDuration := time.Duration(1048576.0 / 1024.0 * float64(time.Second))
	if stats.EstPer1TB.Expected != expectedDuration {
		t.Errorf("expected estimate %v, got %v", expectedDuration, stats.EstPer1TB.Expected)
	}
	if stats.EstPer1TB.BestCase != expectedDuration {
		t.Errorf("expected best case %v, got %v", expectedDuration, stats.EstPer1TB.BestCase)
	}
}
