package store_test

import (
	"context"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

func setupApplicationStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.NewConnection(nil, ":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := store.NewStore(db, nil)
	if err := s.InitCollection(context.Background()); err != nil {
		t.Fatalf("failed to init collection: %v", err)
	}
	return s
}

func TestApplicationStore_ReplaceAll_And_ListOverviews(t *testing.T) {
	s := setupApplicationStore(t)
	ctx := context.Background()

	records := []models.ApplicationVMRecord{
		{AppName: "PostgreSQL", AppDesc: "PG Servers", VMID: "vm-1", VMName: "db-01"},
		{AppName: "PostgreSQL", AppDesc: "PG Servers", VMID: "vm-2", VMName: "db-02"},
		{AppName: "Apache", AppDesc: "Web Servers", VMID: "vm-3", VMName: "web-01"},
	}

	if err := s.Application().ReplaceAll(ctx, records); err != nil {
		t.Fatalf("ReplaceAll() failed: %v", err)
	}

	overviews, err := s.Application().ListOverviews(ctx)
	if err != nil {
		t.Fatalf("ListOverviews() failed: %v", err)
	}

	if len(overviews) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(overviews))
	}

	// Results should be sorted alphabetically
	if overviews[0].Name != "Apache" {
		t.Errorf("expected first app Apache, got %s", overviews[0].Name)
	}
	if overviews[0].VMCount != 1 {
		t.Errorf("expected Apache VMCount 1, got %d", overviews[0].VMCount)
	}

	if overviews[1].Name != "PostgreSQL" {
		t.Errorf("expected second app PostgreSQL, got %s", overviews[1].Name)
	}
	if overviews[1].VMCount != 2 {
		t.Errorf("expected PostgreSQL VMCount 2, got %d", overviews[1].VMCount)
	}
	if overviews[1].VMs[0].ID != "vm-1" || overviews[1].VMs[1].ID != "vm-2" {
		t.Errorf("unexpected PostgreSQL VM IDs: %v", overviews[1].VMs)
	}
}

func TestApplicationStore_ReplaceAll_ClearsPreviousData(t *testing.T) {
	s := setupApplicationStore(t)
	ctx := context.Background()

	// Insert initial data
	initial := []models.ApplicationVMRecord{
		{AppName: "OldApp", AppDesc: "Old", VMID: "vm-1", VMName: "old-vm"},
	}
	if err := s.Application().ReplaceAll(ctx, initial); err != nil {
		t.Fatalf("first ReplaceAll() failed: %v", err)
	}

	// Replace with new data
	updated := []models.ApplicationVMRecord{
		{AppName: "NewApp", AppDesc: "New", VMID: "vm-2", VMName: "new-vm"},
	}
	if err := s.Application().ReplaceAll(ctx, updated); err != nil {
		t.Fatalf("second ReplaceAll() failed: %v", err)
	}

	overviews, err := s.Application().ListOverviews(ctx)
	if err != nil {
		t.Fatalf("ListOverviews() failed: %v", err)
	}

	if len(overviews) != 1 {
		t.Fatalf("expected 1 app after replace, got %d", len(overviews))
	}
	if overviews[0].Name != "NewApp" {
		t.Errorf("expected NewApp, got %s", overviews[0].Name)
	}
}

func TestApplicationStore_ReplaceAll_EmptyRecords(t *testing.T) {
	s := setupApplicationStore(t)
	ctx := context.Background()

	// Insert some data first
	if err := s.Application().ReplaceAll(ctx, []models.ApplicationVMRecord{
		{AppName: "App", AppDesc: "Desc", VMID: "vm-1", VMName: "vm"},
	}); err != nil {
		t.Fatalf("initial ReplaceAll() failed: %v", err)
	}

	// Replace with empty list should clear table
	if err := s.Application().ReplaceAll(ctx, nil); err != nil {
		t.Fatalf("ReplaceAll(nil) failed: %v", err)
	}

	overviews, err := s.Application().ListOverviews(ctx)
	if err != nil {
		t.Fatalf("ListOverviews() failed: %v", err)
	}
	if len(overviews) != 0 {
		t.Errorf("expected 0 apps after empty replace, got %d", len(overviews))
	}
}

func TestApplicationStore_ListOverviews_EmptyTable(t *testing.T) {
	s := setupApplicationStore(t)
	ctx := context.Background()

	overviews, err := s.Application().ListOverviews(ctx)
	if err != nil {
		t.Fatalf("ListOverviews() failed: %v", err)
	}
	if overviews != nil {
		t.Errorf("expected nil for empty table, got %v", overviews)
	}
}
