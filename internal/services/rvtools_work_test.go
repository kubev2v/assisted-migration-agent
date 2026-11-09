package services

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/opa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

type rvtoolsValidationCase struct {
	name    string
	headers []string
	row     []string
	want    string
}

var rvtoolsValidationCases = []rvtoolsValidationCase{
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
}

type rvtoolsSheet struct {
	name         string
	headers, row []string
}

var rvtoolsStorageSheets = []rvtoolsSheet{
	{
		name:    "vInfo",
		headers: []string{"VM ID", "VM", "Cluster", "VI SDK UUID", "Host"},
		row:     []string{"vm-1", "vm1", "cluster1", "vc-1", "esxi1"},
	},
	{
		name:    "vHost",
		headers: []string{"Datacenter", "Cluster", "# Cores", "# CPU", "Object ID", "# Memory", "Model", "Vendor", "Host", "Config status", "VMotion support", "Storage VMotion support"},
		row:     []string{"dc1", "cluster1", "8", "2", "host-1", "32768", "ESXi", "VMware", "esxi1", "green", "true", "true"},
	},
	{
		name:    "vHBA",
		headers: []string{"Host", "Device", "Type", "Driver", "Model", "WWN"},
		row:     []string{"esxi1", "vmhba1", "Fibre Channel", "driver", "adapter", "0000000000000123 0000000000000456"},
	},
	{
		name:    "vMultiPath",
		headers: []string{"Host", "Disk", "Vendor", "Model", "UUID", "Path 1", "Path 2"},
		row:     []string{"esxi1", "naa.001", "NETAPP", "LUN", "000lun-uuid", "vmhba1", "vmhba1"},
	},
}

var _ = Describe("RVTools work", func() {
	for _, tc := range rvtoolsValidationCases {
		It("fails the collection: "+tc.name, func() {
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

			factory, err := newRvtoolWorkFactory(pool, []string{path}, false, dir, nil)
			Expect(err).NotTo(HaveOccurred())

			collector := NewCollectorService(factory)
			defer collector.Stop()
			Expect(collector.Start(ctx)).To(Succeed())
			Eventually(collector.done, 30*time.Second).Should(BeClosed())

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

	It("stores host storage collected from the RVTools export", func() {
		ctx := context.Background()
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "rvtools.xlsx")

		book := excelize.NewFile()
		defer func() { _ = book.Close() }()
		for _, sheet := range rvtoolsStorageSheets {
			_, err := book.NewSheet(sheet.name)
			Expect(err).NotTo(HaveOccurred())
			Expect(book.SetSheetRow(sheet.name, "A1", &sheet.headers)).To(Succeed())
			Expect(book.SetSheetRow(sheet.name, "A2", &sheet.row)).To(Succeed())
		}
		Expect(book.SaveAs(path)).To(Succeed())

		pool := store.NewPool(time.Minute)
		defer pool.Close()
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)

		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())

		pool.Add(mainDB)

		validator, err := opa.NewValidator(map[string]string{"test.rego": "package io.konveyor.forklift.vmware\nconcerns := []"})
		Expect(err).NotTo(HaveOccurred())

		factory, err := newRvtoolWorkFactory(pool, []string{path}, false, dir, validator)
		Expect(err).NotTo(HaveOccurred())

		builder := factory.Build()
		var result models.CollectorResult
		defer func() { _ = builder.Finalize(ctx, result) }()
		for {
			stage, ok := builder.Next()
			if !ok {
				break
			}
			result, err = stage.Work(ctx, result)
			Expect(err).NotTo(HaveOccurred())
		}

		files, err := filepath.Glob(filepath.Join(dir, "collection_*.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(1))

		db, err := store.NewConnection(nil, files[0])
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = db.Close() }()

		inventoryStore := store.NewInventoryStore(db)
		stored, err := inventoryStore.Get(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.Data).To(Equal(result.Inventory))

		const adapters = `[{"device":"vmhba1","type":"Fibre Channel","protocol":"FibreChannel","model":"adapter","driver":"driver","nodeWwn":"0000000000000123","portWwn":"0000000000000456"}]`
		const luns = `[{"canonicalName":"naa.001","lunUuid":"000lun-uuid","vendor":"NETAPP","model":"LUN","adapterDevices":["vmhba1"]}]`
		g := NewGomega(Fail)
		assertCollectedStorage(g, stored.Data, adapters, luns)

		// Per-source inventory survives the temporary-DB append as well as aggregation.
		subset, err := inventoryStore.GetByID(ctx, "vc-1")
		Expect(err).NotTo(HaveOccurred())
		assertCollectedStorage(g, subset.Data, adapters, luns)
	})
})
