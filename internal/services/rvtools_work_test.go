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
		name    string
		headers []string
		row     []string
		want    string
	}{
		{
			name: "no VMs",
			want: "RVTools validation failed: No VMs found in vInfo sheet - cannot build inventory without VM data",
		},
		{
			name: "empty VM ID",
			row:  []string{"", "vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want: "RVTools validation failed: 'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		},
		{
			name: "empty VM name",
			row:  []string{"vm-1", "", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want: "RVTools validation failed: 'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		},
		{
			name: "empty cluster",
			row:  []string{"vm-1", "vm-1", "", "550e8400-e29b-41d4-a716-446655440000"},
			want: "RVTools validation failed: 'Cluster' column is missing or empty in the vInfo sheet - cannot determine cluster membership",
		},
		{
			name: "empty vCenter UUID",
			row:  []string{"vm-1", "vm-1", "cluster-1", ""},
			want: "RVTools validation failed: The RVTools export is missing the 'VI SDK UUID' column on the vInfo sheet, or all values are empty. That column is required to identify your vCenter. Re-export the file using RVTools so the vInfo sheet includes a non-empty VI SDK UUID for at least one VM.",
		},
		{
			name:    "column validation query fails",
			headers: []string{"VM", "Cluster", "VI SDK UUID"},
			row:     []string{"vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			want:    "RVTools file could not be validated",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "rvtools.xlsx")
			book := excelize.NewFile()
			defer func() { g.Expect(book.Close()).To(Succeed()) }()
			g.Expect(book.SetSheetName("Sheet1", "vInfo")).To(Succeed())
			headers := tc.headers
			if headers == nil {
				headers = []string{"VM ID", "VM", "Cluster", "VI SDK UUID"}
			}
			g.Expect(book.SetSheetRow("vInfo", "A1", &headers)).To(Succeed())
			if tc.row != nil {
				g.Expect(book.SetSheetRow("vInfo", "A2", &tc.row)).To(Succeed())
			}
			g.Expect(book.SaveAs(path)).To(Succeed())

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
			g.Expect(status.Error).To(MatchError(tc.want))
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
