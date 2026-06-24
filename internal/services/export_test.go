package services_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"io"
	"strings"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func newTestService(t *testing.T) (context.Context, *services.ExportService, *sql.DB) {
	t.Helper()

	ctx := context.Background()
	db, err := store.NewDB(nil, ":memory:")
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st := store.NewStore(db, test.NewMockValidator())
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := test.InsertVMs(ctx, db); err != nil {
		t.Fatalf("insert vms: %v", err)
	}

	return ctx, services.NewExportService(st), db
}

func TestWriteZip_scopeFiles(t *testing.T) {
	tests := []struct {
		scope string
		files []string
	}{
		{scope: "overview", files: []string{"overview.csv"}},
		{scope: "hosts", files: []string{"hosts.csv"}},
		{scope: "clusters", files: []string{"clusters.csv"}},
		{scope: "datastores", files: []string{"datastores.csv"}},
		{scope: "vms", files: []string{"vms.csv"}},
		{scope: "network", files: []string{"networks.csv"}},
		{scope: "utilization", files: []string{"vm_utilization.csv", "cluster_utilization.csv"}},
		{scope: "applications", files: []string{"applications.csv"}},
		{scope: "groups", files: []string{"groups.csv"}},
		{scope: "inspection", files: []string{"inspection.csv"}},
		{scope: "storage-forecast", files: []string{"storage-forecast.csv"}},
	}

	ctx, svc, _ := newTestService(t)

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			zipFiles, err := writeZip(ctx, svc, []string{tt.scope})
			if err != nil {
				t.Fatalf("WriteZip: %v", err)
			}

			files, err := readZip(zipFiles)
			if err != nil {
				t.Fatalf("readZip: %v", err)
			}

			for _, name := range tt.files {
				body, ok := files[name]
				if !ok {
					t.Fatalf("missing %q in zip", name)
				}
				if len(body) == 0 {
					t.Fatalf("%q is empty", name)
				}
			}
		})
	}
}

func TestWriteZip_allScopes(t *testing.T) {
	allScopes := []string{
		"overview", "hosts", "clusters", "datastores", "vms", "network",
		"utilization", "applications", "groups", "inspection", "storage-forecast",
	}
	wantFiles := []string{
		"overview.csv", "hosts.csv", "clusters.csv", "datastores.csv", "vms.csv",
		"networks.csv", "vm_utilization.csv", "cluster_utilization.csv",
		"applications.csv", "groups.csv", "inspection.csv", "storage-forecast.csv",
	}

	ctx, svc, _ := newTestService(t)

	zipFiles, err := writeZip(ctx, svc, allScopes)
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}

	files, err := readZip(zipFiles)
	if err != nil {
		t.Fatalf("readZip: %v", err)
	}
	if len(files) != len(wantFiles) {
		t.Fatalf("got %d zip entries, want %d", len(files), len(wantFiles))
	}
	for _, name := range wantFiles {
		if _, ok := files[name]; !ok {
			t.Fatalf("missing %q in zip", name)
		}
	}
}

func TestWriteZip_validArchive(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	data, err := writeZip(ctx, svc, []string{"overview"})
	if err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	if len(data) < 2 || data[0] != 0x50 || data[1] != 0x4b {
		t.Fatalf("expected ZIP magic bytes PK, got %v", data[:min(2, len(data))])
	}
}

func TestWriteZip_errors(t *testing.T) {
	tests := []struct {
		name    string
		scopes  []string
		setup   func(context.Context, *sql.DB) context.Context
		wantErr string
	}{
		{
			name:   "cancelled context",
			scopes: []string{"overview"},
			setup: func(ctx context.Context, _ *sql.DB) context.Context {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				return cancelled
			},
			wantErr: context.Canceled.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, db := newTestService(t)
			if tt.setup != nil {
				ctx = tt.setup(ctx, db)
			}

			err := svc.WriteZip(ctx, tt.scopes, &bytes.Buffer{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestWriteZip_overview(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	files, err := readZipFromService(ctx, svc, []string{"overview"})
	if err != nil {
		t.Fatalf("export overview: %v", err)
	}

	t.Run("row count", func(t *testing.T) {
		count, err := csvDataRowCount(files["overview.csv"])
		if err != nil {
			t.Fatalf("parse csv: %v", err)
		}
		if count != len(test.VMs) {
			t.Fatalf("got %d data rows, want %d", count, len(test.VMs))
		}
	})

	t.Run("migration_status", func(t *testing.T) {
		tests := []struct {
			vmID   string
			status string
		}{
			{vmID: "vm-001", status: "Ready"},
			{vmID: "vm-003", status: "Review"},
			{vmID: "vm-007", status: "Blocked"},
		}

		rows, err := csvRowsByColumn(files["overview.csv"], "id")
		if err != nil {
			t.Fatalf("parse csv: %v", err)
		}

		for _, tt := range tests {
			t.Run(tt.vmID, func(t *testing.T) {
				row, ok := rows[tt.vmID]
				if !ok {
					t.Fatalf("vm %q not found in overview.csv", tt.vmID)
				}
				if row["migration_status"] != tt.status {
					t.Fatalf("migration_status = %q, want %q", row["migration_status"], tt.status)
				}
			})
		}
	})
}

func TestWriteZip_csvInjection(t *testing.T) {
	ctx, svc, db := newTestService(t)
	if _, err := db.ExecContext(ctx, `UPDATE vinfo SET "VM" = '=1+1' WHERE "VM ID" = 'vm-001'`); err != nil {
		t.Fatalf("update vm name: %v", err)
	}

	files, err := readZipFromService(ctx, svc, []string{"overview"})
	if err != nil {
		t.Fatalf("export overview: %v", err)
	}

	rows, err := csvRowsByColumn(files["overview.csv"], "id")
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	row, ok := rows["vm-001"]
	if !ok {
		t.Fatal("vm-001 not found in overview.csv")
	}
	if row["name"] != "'=1+1" {
		t.Fatalf("name = %q, want %q", row["name"], "'=1+1")
	}
}

func TestWriteZip_network(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	files, err := readZipFromService(ctx, svc, []string{"network"})
	if err != nil {
		t.Fatalf("export network: %v", err)
	}

	count, err := csvDataRowCount(files["networks.csv"])
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if count != len(test.NICs) {
		t.Fatalf("got %d NIC rows, want %d", count, len(test.NICs))
	}
}

func TestWriteZip_utilization(t *testing.T) {
	tests := []struct {
		name               string
		seedUtilization    bool
		wantVMRows         int
		wantClusterRowsMin int
	}{
		{
			name:               "no rightsizing report",
			wantVMRows:         0,
			wantClusterRowsMin: 0,
		},
		{
			name:               "with rightsizing report",
			seedUtilization:    true,
			wantVMRows:         len(test.Utilizations),
			wantClusterRowsMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, db := newTestService(t)
			if tt.seedUtilization {
				if err := test.InsertVMUtilization(ctx, db); err != nil {
					t.Fatalf("insert utilization: %v", err)
				}
			}

			files, err := readZipFromService(ctx, svc, []string{"utilization"})
			if err != nil {
				t.Fatalf("export utilization: %v", err)
			}

			for _, name := range []string{"vm_utilization.csv", "cluster_utilization.csv"} {
				if _, ok := files[name]; !ok {
					t.Fatalf("missing %q", name)
				}
			}

			vmRows, err := csvDataRowCount(files["vm_utilization.csv"])
			if err != nil {
				t.Fatalf("parse vm_utilization.csv: %v", err)
			}
			if vmRows != tt.wantVMRows {
				t.Fatalf("vm_utilization rows = %d, want %d", vmRows, tt.wantVMRows)
			}

			clusterRows, err := csvDataRowCount(files["cluster_utilization.csv"])
			if err != nil {
				t.Fatalf("parse cluster_utilization.csv: %v", err)
			}
			if clusterRows < tt.wantClusterRowsMin {
				t.Fatalf("cluster_utilization rows = %d, want at least %d", clusterRows, tt.wantClusterRowsMin)
			}

			if !tt.seedUtilization && !strings.Contains(string(files["vm_utilization.csv"]), "vm_name") {
				t.Fatal("vm_utilization.csv missing header")
			}
		})
	}
}

func readZipFromService(ctx context.Context, svc *services.ExportService, scopes []string) (map[string][]byte, error) {
	data, err := writeZip(ctx, svc, scopes)
	if err != nil {
		return nil, err
	}
	return readZip(data)
}

func writeZip(ctx context.Context, svc *services.ExportService, scopes []string) ([]byte, error) {
	var buf bytes.Buffer
	if err := svc.WriteZip(ctx, scopes, &buf); err != nil {
		return nil, err
	}
	if buf.Len() == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return buf.Bytes(), nil
}

func readZip(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	files := make(map[string][]byte, len(reader.File))
	for _, f := range reader.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}

		files[f.Name] = body
	}
	return files, nil
}

func csvDataRowCount(data []byte) (int, error) {
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		return 0, err
	}
	if len(records) <= 1 {
		return 0, nil
	}
	return len(records) - 1, nil
}

func csvRowsByColumn(data []byte, keyColumn string) (map[string]map[string]string, error) {
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, io.ErrUnexpectedEOF
	}

	header := records[0]
	keyIndex := -1
	for i, col := range header {
		if col == keyColumn {
			keyIndex = i
			break
		}
	}
	if keyIndex < 0 {
		return nil, io.ErrUnexpectedEOF
	}

	rows := make(map[string]map[string]string, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]string, len(header))
		for i, col := range header {
			row[col] = record[i]
		}
		rows[record[keyIndex]] = row
	}
	return rows, nil
}
