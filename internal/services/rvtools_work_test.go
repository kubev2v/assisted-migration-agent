package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	v1api "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestRVToolsValidationErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  []byte
		sheet    string
		truncate bool
		headers  []string
		row      []string
		want     string
	}{
		{
			name: "no VMs",
			want: "No VMs found in vInfo sheet - cannot build inventory without VM data",
		},
		{
			name: "empty VM ID",
			row:  []string{"", "vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want: "'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		},
		{
			name: "empty VM name",
			row:  []string{"vm-1", "", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want: "'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		},
		{
			name: "empty cluster",
			row:  []string{"vm-1", "vm-1", "", "550e8400-e29b-41d4-a716-446655440000"},
			want: "'Cluster' column is missing or empty in the vInfo sheet - cannot determine cluster membership",
		},
		{
			name: "empty vCenter UUID",
			row:  []string{"vm-1", "vm-1", "cluster-1", ""},
			want: "The RVTools export is missing the 'VI SDK UUID' column on the vInfo sheet, or all values are empty. That column is required to identify your vCenter. Re-export the file using RVTools so the vInfo sheet includes a non-empty VI SDK UUID for at least one VM.",
		},
		{
			name:    "column validation query fails",
			headers: []string{"VM", "Cluster", "VI SDK UUID"},
			row:     []string{"vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want:    duckdb_parser.RVToolsFailureMessage,
		},
		{
			name:    "query failure alongside a readable issue",
			headers: []string{"VM", "Cluster", "VI SDK UUID"},
			row:     []string{"", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want:    duckdb_parser.RVToolsFailureMessage,
		},
		{name: "JSON", content: []byte("{}"), want: "The file is corrupted or not a valid Excel file"},
		{name: "empty file", content: []byte{}, want: "The file is corrupted or not a valid Excel file"},
		{name: "truncated", truncate: true, want: "The file is corrupted or not a valid Excel file"},
		{name: "missing vInfo", sheet: "vHost", want: "File is not a valid RVTools export (missing required 'vInfo' sheet)"},
		{name: "multiple issues", row: []string{"", "", "cluster-1", "uuid"},
			want: "'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified\n'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "rvtools.xlsx")
			book := excelize.NewFile()
			defer func() { g.Expect(book.Close()).To(Succeed()) }()
			sheet := tc.sheet
			if sheet == "" {
				sheet = "vInfo"
			}
			g.Expect(book.SetSheetName("Sheet1", sheet)).To(Succeed())
			headers := tc.headers
			if headers == nil {
				headers = []string{"VM ID", "VM", "Cluster", "VI SDK UUID"}
			}
			g.Expect(book.SetSheetRow(sheet, "A1", &headers)).To(Succeed())
			if tc.row != nil {
				g.Expect(book.SetSheetRow(sheet, "A2", &tc.row)).To(Succeed())
			}
			g.Expect(book.SaveAs(path)).To(Succeed())
			if tc.content != nil {
				g.Expect(os.WriteFile(path, tc.content, 0600)).To(Succeed())
			}
			if tc.truncate {
				data, err := os.ReadFile(path)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(os.Truncate(path, int64(len(data)/2))).To(Succeed())
			}

			pool := store.NewPool(5 * time.Minute)
			defer pool.Close()
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
			pool.Add(mainDB)

			factory, err := newRvtoolWorkFactory(pool, []string{path}, dir, nil)
			g.Expect(err).NotTo(HaveOccurred())
			collector := NewCollectorService(factory)
			defer collector.Stop()
			g.Expect(collector.Start(ctx)).To(Succeed())
			select {
			case <-collector.done:
			case <-time.After(30 * time.Second):
				t.Fatal("RVTools collection did not finish")
			}

			status := collector.GetStatus()
			g.Expect(status.State).To(Equal(models.CollectorStateError))
			g.Expect(status.Error).To(HaveOccurred())
			var processing *duckdb_parser.RVToolsProcessingError
			g.Expect(errors.As(status.Error, &processing)).To(BeTrue())
			g.Expect(processing.Stage).To(Equal(duckdb_parser.RVToolsStageParsing))
			g.Expect(processing.Filename).To(Equal(path))
			analyzerStatus := v1api.NewCollectorStatus(status)
			raw, marshalErr := json.Marshal(analyzerStatus)
			g.Expect(marshalErr).NotTo(HaveOccurred())
			var fields map[string]any
			g.Expect(json.Unmarshal(raw, &fields)).To(Succeed())
			g.Expect(fields["error"]).To(Equal(tc.want))
			g.Expect(string(raw)).NotTo(ContainSubstring(dir))
			st, err := mainDB.Store()
			g.Expect(err).NotTo(HaveOccurred())
			collections, err := st.Collection().List(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(collections).To(HaveLen(1))
			g.Expect(collections[0].State).To(Equal(models.CollectionStateFailed))
			g.Expect(collections[0].Error).To(Equal(tc.want))
			g.Expect(pool.List()).To(HaveLen(1))
			_, err = os.Stat(path)
			g.Expect(os.IsNotExist(err)).To(BeTrue(), "uploaded workbook should be removed")
		})
	}
}

func TestRVToolsCollectorStatusDoesNotExposeUnknownCause(t *testing.T) {
	g := NewWithT(t)
	cause := fmt.Errorf("database query SELECT * FROM /var/lib/agent/private.duckdb")
	status := models.CollectorStatus{State: models.CollectorStateError, Error: cause}
	message := v1api.NewCollectorStatus(status).Error
	g.Expect(message).To(HaveValue(Equal(duckdb_parser.RVToolsFailureMessage)))
	g.Expect(status.Error).To(BeIdenticalTo(cause))

	workbook := &duckdb_parser.XLSXError{Cause: fmt.Errorf("query /private/upload.xlsx: No xl/workbook.xml found")}
	for _, stage := range []duckdb_parser.RVToolsStage{duckdb_parser.RVToolsStageParsing, duckdb_parser.RVToolsStageInitialization, duckdb_parser.RVToolsStageCleanup} {
		status.Error = &duckdb_parser.RVToolsProcessingError{Stage: stage, Filename: "/private/upload.xlsx", Cause: workbook}
		want := duckdb_parser.RVToolsFailureMessage
		if stage == duckdb_parser.RVToolsStageParsing {
			want = "The file is corrupted or not a valid Excel file"
		}
		g.Expect(v1api.NewCollectorStatus(status).Error).To(HaveValue(Equal(want)))
	}
}
