package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func setupForecastStore(t *testing.T) *store.Store {
	t.Helper()
	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		t.Fatalf("failed to create duckdb connector: %v", err)
	}

	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	s := store.NewStore(db, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return s
}

func TestForecastStore_InsertAndListRuns(t *testing.T) {
	s := setupForecastStore(t)
	ctx := context.Background()

	run := models.BenchmarkRun{
		SessionID:      1,
		PairName:       "test-pair",
		SourceDS:       "ds-source",
		TargetDS:       "ds-target",
		Iteration:      1,
		DiskSizeGB:     10,
		DurationSec:    5.5,
		ThroughputMBps: 1861.8,
		Method:         "vm_native",
	}

	if err := s.Forecast().InsertRun(ctx, run); err != nil {
		t.Fatalf("failed to insert run: %v", err)
	}

	runs, err := s.Forecast().ListRuns(ctx, "test-pair")
	if err != nil {
		t.Fatalf("failed to list runs: %v", err)
	}

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	if runs[0].PairName != "test-pair" {
		t.Errorf("expected pair name 'test-pair', got %q", runs[0].PairName)
	}
	if runs[0].ThroughputMBps != 1861.8 {
		t.Errorf("expected throughput 1861.8, got %f", runs[0].ThroughputMBps)
	}
}

func TestForecastStore_DeleteRun(t *testing.T) {
	s := setupForecastStore(t)
	ctx := context.Background()

	run := models.BenchmarkRun{
		SessionID:      1,
		PairName:       "test-pair",
		SourceDS:       "ds-source",
		TargetDS:       "ds-target",
		Iteration:      1,
		DiskSizeGB:     10,
		DurationSec:    5.5,
		ThroughputMBps: 1861.8,
	}

	if err := s.Forecast().InsertRun(ctx, run); err != nil {
		t.Fatalf("failed to insert run: %v", err)
	}

	// Get the run to find its ID
	runs, _ := s.Forecast().ListRuns(ctx, "test-pair")
	if len(runs) == 0 {
		t.Fatal("expected at least one run")
	}

	// Delete it
	if err := s.Forecast().DeleteRun(ctx, runs[0].ID); err != nil {
		t.Fatalf("failed to delete run: %v", err)
	}

	// Verify deleted
	runs, _ = s.Forecast().ListRuns(ctx, "test-pair")
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after delete, got %d", len(runs))
	}
}

func TestForecastStore_DeleteRunNotFound(t *testing.T) {
	s := setupForecastStore(t)
	ctx := context.Background()

	err := s.Forecast().DeleteRun(ctx, 99999)
	if err == nil {
		t.Error("expected error when deleting non-existent run")
	}
}

func TestForecastStore_NextSessionID(t *testing.T) {
	s := setupForecastStore(t)
	ctx := context.Background()

	id1, err := s.Forecast().NextSessionID(ctx)
	if err != nil {
		t.Fatalf("failed to get session ID: %v", err)
	}

	id2, err := s.Forecast().NextSessionID(ctx)
	if err != nil {
		t.Fatalf("failed to get second session ID: %v", err)
	}

	if id2 <= id1 {
		t.Errorf("expected second session ID > first, got %d <= %d", id2, id1)
	}
}
