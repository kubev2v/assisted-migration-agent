package services

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("RVTools Validation Errors", func() {
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
		It(tc.name, func() {
			ctx := context.Background()
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "rvtools.xlsx")
			book := excelize.NewFile()
			defer func() { Expect(book.Close()).To(Succeed()) }()
			Expect(book.SetSheetName("Sheet1", "vInfo")).To(Succeed())
			headers := tc.headers
			if headers == nil {
				headers = []string{"VM ID", "VM", "Cluster", "VI SDK UUID"}
			}
			Expect(book.SetSheetRow("vInfo", "A1", &headers)).To(Succeed())
			if tc.row != nil {
				Expect(book.SetSheetRow("vInfo", "A2", &tc.row)).To(Succeed())
			}
			Expect(book.SaveAs(path)).To(Succeed())

			pool := store.NewPool(5 * time.Minute)
			defer pool.Close()
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
			pool.Add(mainDB)

			factory, err := newRvtoolWorkFactory(pool, []string{path}, dir, nil)
			Expect(err).NotTo(HaveOccurred())
			collector := NewCollectorService(factory)
			defer collector.Stop()
			Expect(collector.Start(ctx)).To(Succeed())
			select {
			case <-collector.done:
			case <-time.After(30 * time.Second):
				Fail("RVTools collection did not finish")
			}

			status := collector.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateError))
			Expect(status.Error).To(MatchError(tc.want))
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())
			collections, err := st.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(HaveLen(1))
			Expect(collections[0].State).To(Equal(models.CollectionStateFailed))
			Expect(collections[0].Error).To(Equal(tc.want))
			Expect(pool.List()).To(HaveLen(1))
			_, err = os.Stat(path)
			Expect(os.IsNotExist(err)).To(BeTrue(), "uploaded workbook should be removed")
		})
	}
})
