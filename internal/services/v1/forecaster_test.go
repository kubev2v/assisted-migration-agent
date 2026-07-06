package v1_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v1 "github.com/kubev2v/assisted-migration-agent/internal/services/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	db, err := store.NewConnection(nil, filepath.Join(dir, "agent.duckdb"))
	if err != nil {
		t.Fatalf("failed to create duckdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := store.NewStore(db, nil)
	if err := migrations.RunMain(context.Background(), db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	return s
}

func TestForecasterService_InitialState(t *testing.T) {
	s := setupTestStore(t)
	svc := v1.NewForecasterService(s, 10, nil)

	status := svc.GetStatus()
	if status.State != models.ForecasterStateReady {
		t.Errorf("expected ready state, got %s", status.State)
	}

	if svc.IsBusy() {
		t.Error("expected not busy")
	}
}

func TestForecasterService_StartWithEmptyPairs(t *testing.T) {
	s := setupTestStore(t)
	svc := v1.NewForecasterService(s, 10, nil)

	req := models.ForecastRequest{}

	err := svc.Start(context.Background(), req)
	if err == nil {
		t.Error("expected error with empty pairs")
	}
}

func TestForecasterService_StopWhenNotRunning(t *testing.T) {
	s := setupTestStore(t)
	svc := v1.NewForecasterService(s, 10, nil)

	err := svc.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running forecaster")
	}
}

func TestForecasterService_PairLimitEnforced(t *testing.T) {
	s := setupTestStore(t)
	svc := v1.NewForecasterService(s, 2, nil)

	req := models.ForecastRequest{
		Pairs: []models.DatastorePair{
			{Name: "p1", SourceDatastore: "ds1", TargetDatastore: "ds2"},
			{Name: "p2", SourceDatastore: "ds3", TargetDatastore: "ds4"},
			{Name: "p3", SourceDatastore: "ds5", TargetDatastore: "ds6"},
		},
	}

	err := svc.Start(context.Background(), req)
	if err == nil {
		t.Error("expected error when exceeding pair limit")
	}
}
