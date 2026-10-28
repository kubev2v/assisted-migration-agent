package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestRVToolsValidationErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		headers   []string
		row       []string
		want      string
		content   []byte
		sheet     string
		technical bool
		truncate  bool
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
			name: "multiple validation errors",
			row:  []string{"", "", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want: "'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified\n'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
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
			want:    `could not validate column "VM ID":`, technical: true,
		},
		{
			name:      "readable and technical validation errors",
			headers:   []string{"VM", "Cluster", "VI SDK UUID"},
			row:       []string{"", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			technical: true,
			want:      "'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		},
		{
			name: "JSON renamed xlsx", content: []byte(`{"VM":"vm-1"}`),
			want: "zip: not a valid zip file", technical: true,
		},
		{
			name: "text renamed xlsx", content: []byte("not an Excel workbook"),
			want: "zip: not a valid zip file", technical: true,
		},
		{
			name: "empty file", content: []byte{},
			want: "zip: not a valid zip file", technical: true,
		},
		{
			name: "truncated workbook", truncate: true,
			want: "zip: not a valid zip file", technical: true,
		},
		{
			name: "missing vInfo", sheet: "vHost",
			want: "reading vInfo headers: sheet vInfo does not exist", technical: true,
		},
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
			if tc.technical {
				g.Expect(status.Error).To(HaveOccurred())
				g.Expect(status.Error.Error()).To(ContainSubstring(tc.want))
				if tc.headers != nil {
					g.Expect(status.Error.Error()).To(ContainSubstring(`could not validate column "VM ID":`))
				}
			} else {
				g.Expect(status.Error).To(MatchError(tc.want))
			}
			st, err := mainDB.Store()
			g.Expect(err).NotTo(HaveOccurred())
			collections, err := st.Collection().List(ctx)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(collections).To(HaveLen(1))
			g.Expect(collections[0].State).To(Equal(models.CollectionStateFailed))
			g.Expect(collections[0].Error).To(Equal(status.Error.Error()))
			g.Expect(pool.List()).To(HaveLen(1))
			_, err = os.Stat(path)
			g.Expect(os.IsNotExist(err)).To(BeTrue(), "uploaded workbook should be removed")
		})
	}
}
