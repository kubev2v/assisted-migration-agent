package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubev2v/migration-planner/pkg/opa"
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

func TestRVToolsWork_StoredHostStorage(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "rvtools.xlsx")
	book := excelize.NewFile()
	defer func() { _ = book.Close() }()
	for _, sheet := range []struct {
		name         string
		headers, row []string
	}{
		{"vInfo", []string{"VM ID", "VM", "Cluster", "VI SDK UUID", "Host"}, []string{"vm-1", "vm1", "cluster1", "vc-1", "esxi1"}},
		{"vHost", []string{"Datacenter", "Cluster", "# Cores", "# CPU", "Object ID", "# Memory", "Model", "Vendor", "Host", "Config status", "VMotion support", "Storage VMotion support"}, []string{"dc1", "cluster1", "8", "2", "host-1", "32768", "ESXi", "VMware", "esxi1", "green", "true", "true"}},
		{"vHBA", []string{"Host", "Device", "Type", "Driver", "Model", "WWN"}, []string{"esxi1", "vmhba1", "Fibre Channel", "driver", "adapter", "0000000000000123 0000000000000456"}},
		{"vMultiPath", []string{"Host", "Disk", "Vendor", "Model", "UUID", "Path 1", "Path 2"}, []string{"esxi1", "naa.001", "NETAPP", "LUN", "000lun-uuid", "vmhba1", "vmhba1"}},
	} {
		_, err := book.NewSheet(sheet.name)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(book.SetSheetRow(sheet.name, "A1", &sheet.headers)).To(Succeed())
		g.Expect(book.SetSheetRow(sheet.name, "A2", &sheet.row)).To(Succeed())
	}
	g.Expect(book.SaveAs(path)).To(Succeed())
	pool := store.NewPool(time.Minute)
	defer pool.Close()
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
	pool.Add(mainDB)
	validator, err := opa.NewValidator(map[string]string{"test.rego": "package io.konveyor.forklift.vmware\nconcerns := []"})
	g.Expect(err).NotTo(HaveOccurred())
	factory, err := newRvtoolWorkFactory(pool, []string{path}, dir, validator)
	g.Expect(err).NotTo(HaveOccurred())
	builder := factory.Build()
	var result models.CollectorResult
	defer func() { _ = builder.Finalize(ctx, result) }()
	for {
		stage, ok := builder.Next()
		if !ok {
			break
		}
		result, err = stage.Work(ctx, result)
		g.Expect(err).NotTo(HaveOccurred())
	}
	files, err := filepath.Glob(filepath.Join(dir, "collection_*.duckdb"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(files).To(HaveLen(1))
	db, err := store.NewConnection(nil, files[0])
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = db.Close() }()
	inventoryStore := store.NewInventoryStore(db)
	stored, err := inventoryStore.Get(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(stored.Data).To(Equal(result.Inventory))
	const adapters = `[{"device":"vmhba1","type":"Fibre Channel","protocol":"FibreChannel","model":"adapter","driver":"driver","nodeWwn":"0000000000000123","portWwn":"0000000000000456"}]`
	const luns = `[{"canonicalName":"naa.001","lunUuid":"000lun-uuid","vendor":"NETAPP","model":"LUN","adapterDevices":["vmhba1"]}]`
	assertCollectedStorage(t, stored.Data, adapters, luns)
	// Per-source inventory survives the temporary-DB append as well as aggregation.
	subset, err := inventoryStore.GetByID(ctx, "vc-1")
	g.Expect(err).NotTo(HaveOccurred())
	assertCollectedStorage(t, subset.Data, adapters, luns)
}
