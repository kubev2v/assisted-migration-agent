package services_test

import (
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"

	"context"
	"database/sql"

	"github.com/duckdb/duckdb-go/v2"
)

func setupTestStore(t *testing.T) *store.Store {
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

func TestForecasterService_InitialState(t *testing.T) {
	s := setupTestStore(t)
	svc := services.NewForecasterService(s, 10)

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
	svc := services.NewForecasterService(s, 10)

	req := models.ForecastRequest{
		Credentials: models.Credentials{
			URL:      "https://vcenter.example.com",
			Username: "admin",
			Password: "pass",
		},
	}

	err := svc.Start(context.Background(), req)
	if err == nil {
		t.Error("expected error with empty pairs")
	}
}

func TestForecasterService_StopWhenNotRunning(t *testing.T) {
	s := setupTestStore(t)
	svc := services.NewForecasterService(s, 10)

	err := svc.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running forecaster")
	}
}

func TestForecasterService_PairLimitEnforced(t *testing.T) {
	s := setupTestStore(t)
	svc := services.NewForecasterService(s, 2)

	req := models.ForecastRequest{
		Credentials: models.Credentials{
			URL:      "https://vcenter.example.com",
			Username: "admin",
			Password: "pass",
		},
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
