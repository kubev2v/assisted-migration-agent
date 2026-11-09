package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/opa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	v1api "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

type rvtoolsValidationCase struct {
	content               []byte
	sheet                 string
	truncate              bool
	initializationFailure bool
	headers               []string
	row                   []string
}

var _ = Describe("RVTools validation errors", func() {
	var (
		ctx    context.Context
		dir    string
		pool   *store.Pool
		mainDB *store.Database
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		pool = store.NewPool(5 * time.Minute)
		DeferCleanup(pool.Close)

		var err error
		mainDB, err = pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
	})

	DescribeTable("reports safe errors and cleans up the failed collection",
		func(tc rvtoolsValidationCase, want string) {
			path := filepath.Join(dir, "rvtools.xlsx")
			book := excelize.NewFile()
			DeferCleanup(book.Close)

			sheet := tc.sheet
			if sheet == "" {
				sheet = "vInfo"
			}

			Expect(book.SetSheetName("Sheet1", sheet)).To(Succeed())
			headers := tc.headers
			if headers == nil {
				headers = []string{"VM ID", "VM", "Cluster", "VI SDK UUID"}
			}

			Expect(book.SetSheetRow(sheet, "A1", &headers)).To(Succeed())
			if tc.row != nil {
				Expect(book.SetSheetRow(sheet, "A2", &tc.row)).To(Succeed())
			}

			Expect(book.SaveAs(path)).To(Succeed())

			if tc.content != nil {
				Expect(os.WriteFile(path, tc.content, 0600)).To(Succeed())
			}

			if tc.truncate {
				data, err := os.ReadFile(path)
				Expect(err).NotTo(HaveOccurred())
				Expect(os.Truncate(path, int64(len(data)/2))).To(Succeed())
			}

			dataDir := dir
			if tc.initializationFailure {
				dataDir = path // An existing file cannot be used as the database directory.
			}
			factory, err := newRvtoolWorkFactory(pool, []string{path}, false, dataDir, nil)
			Expect(err).NotTo(HaveOccurred())

			collector := NewCollectorService(factory)
			DeferCleanup(collector.Stop)

			Expect(collector.Start(ctx)).To(Succeed())
			Eventually(collector.done, 30*time.Second).Should(BeClosed())

			status := collector.GetStatus()
			Expect(status.State).To(Equal(models.CollectorStateError))
			Expect(status.Error).To(HaveOccurred())

			var processing *duckdb_parser.RVToolsProcessingError
			Expect(errors.As(status.Error, &processing)).To(BeTrue())

			if tc.initializationFailure {
				Expect(processing.Stage).To(Equal(duckdb_parser.RVToolsStageInitialization))
			} else {
				Expect(processing.Stage).To(Equal(duckdb_parser.RVToolsStageParsing))
				Expect(processing.Filename).To(Equal(path))
			}

			analyzerStatus := v1api.NewCollectorStatus(status)
			raw, marshalErr := json.Marshal(analyzerStatus)
			Expect(marshalErr).NotTo(HaveOccurred())
			var fields map[string]any
			Expect(json.Unmarshal(raw, &fields)).To(Succeed())
			Expect(fields["error"]).To(Equal(want))
			Expect(string(raw)).NotTo(ContainSubstring(dir))

			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(HaveLen(1))
			Expect(collections[0].State).To(Equal(models.CollectionStateFailed))
			Expect(collections[0].Error).To(Equal(want))
			Expect(pool.List()).To(HaveLen(1))

			_, err = os.Stat(path)
			Expect(os.IsNotExist(err)).To(BeTrue(), "uploaded workbook should be removed")
		},
		Entry(
			"database initialization fails",
			rvtoolsValidationCase{
				initializationFailure: true,
			},
			"Could not start processing the RVTools file",
		),

		Entry(
			"no VMs",
			rvtoolsValidationCase{},
			"No VMs found in vInfo sheet - cannot build inventory without VM data",
		),

		Entry(
			"empty VM ID",
			rvtoolsValidationCase{
				row: []string{"", "vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			},
			"'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		),

		Entry(
			"empty VM name",
			rvtoolsValidationCase{
				row: []string{"vm-1", "", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			},
			"'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		),

		Entry(
			"empty cluster",
			rvtoolsValidationCase{
				row: []string{"vm-1", "vm-1", "", "550e8400-e29b-41d4-a716-446655440000"},
			},
			"'Cluster' column is missing or empty in the vInfo sheet - cannot determine cluster membership",
		),

		Entry(
			"empty vCenter UUID",
			rvtoolsValidationCase{
				row: []string{"vm-1", "vm-1", "cluster-1", ""},
			},
			"The RVTools export is missing the 'VI SDK UUID' column on the vInfo sheet, or all values are empty. "+
				"That column is required to identify your vCenter. "+
				"Re-export the file using RVTools so the vInfo sheet includes a non-empty VI SDK UUID for at least one VM.",
		),

		Entry(
			"column validation query fails",
			rvtoolsValidationCase{
				headers: []string{"VM", "Cluster", "VI SDK UUID"},
				row:     []string{"vm-1", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			},
			duckdb_parser.RVToolsFailureMessage,
		),

		Entry(
			"query failure alongside a readable issue",
			rvtoolsValidationCase{
				headers: []string{"VM", "Cluster", "VI SDK UUID"},
				row:     []string{"", "cluster-1", "550e8400-e29b-41d4-a716-446655440000"},
			},
			duckdb_parser.RVToolsFailureMessage,
		),

		Entry(
			"JSON",
			rvtoolsValidationCase{content: []byte("{}")},
			"The file is corrupted or not a valid Excel file",
		),

		Entry(
			"empty file",
			rvtoolsValidationCase{content: []byte{}},
			"The file is corrupted or not a valid Excel file",
		),

		Entry(
			"truncated",
			rvtoolsValidationCase{truncate: true},
			"The file is corrupted or not a valid Excel file",
		),

		Entry(
			"missing vInfo",
			rvtoolsValidationCase{sheet: "vHost"},
			"File is not a valid RVTools export (missing required 'vInfo' sheet)",
		),

		Entry(
			"multiple issues",
			rvtoolsValidationCase{row: []string{"", "", "cluster-1", "uuid"}},
			"'VM ID' column is missing or empty in the vInfo sheet - VMs cannot be identified\n"+
				"'VM' column is missing or empty in the vInfo sheet - VMs cannot be identified",
		),
	)
})

var _ = DescribeTable("RVTools safe status errors",
	func(cause error, want string) {
		status := models.CollectorStatus{State: models.CollectorStateError, Error: cause}
		Expect(v1api.NewCollectorStatus(status).Error).To(HaveValue(Equal(want)))
		Expect(status.Error).To(BeIdenticalTo(cause))

		ctx := context.Background()
		dir := GinkgoT().TempDir()
		pool := store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		factory, err := newRvtoolWorkFactory(pool, nil, false, dir, nil)
		Expect(err).NotTo(HaveOccurred())

		builder := factory.Build()
		provision, ok := builder.Next()
		Expect(ok).To(BeTrue())
		result, err := provision.Work(ctx, models.CollectorResult{})
		Expect(err).NotTo(HaveOccurred())

		result.Err = cause
		Expect(builder.Finalize(ctx, result)).To(Succeed())

		st, err := mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		collections, err := st.Collection().List(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(collections).To(HaveLen(1))
		Expect(collections[0].State).To(Equal(models.CollectionStateFailed))
		Expect(collections[0].Error).To(Equal(want))
	},
	Entry(
		"unknown cause",
		fmt.Errorf("database query SELECT * FROM /private/agent.duckdb"),
		duckdb_parser.RVToolsFailureMessage,
	),

	Entry(
		"parsing a corrupt workbook",
		&duckdb_parser.RVToolsProcessingError{
			Stage:    duckdb_parser.RVToolsStageParsing,
			Filename: "/private/upload.xlsx",
			Cause:    &duckdb_parser.XLSXError{Cause: fmt.Errorf("query /private/upload.xlsx: No xl/workbook.xml found")},
		},
		"The file is corrupted or not a valid Excel file",
	),

	Entry(
		"initializing collection",
		&duckdb_parser.RVToolsProcessingError{
			Stage: duckdb_parser.RVToolsStageInitialization,
			Cause: &duckdb_parser.XLSXError{Cause: fmt.Errorf("query /private/upload.xlsx: No xl/workbook.xml found")},
		},
		"Could not start processing the RVTools file",
	),

	Entry(
		"cleaning up collection",
		&duckdb_parser.RVToolsProcessingError{
			Stage: duckdb_parser.RVToolsStageCleanup,
			Cause: &duckdb_parser.XLSXError{Cause: fmt.Errorf("query /private/upload.xlsx: No xl/workbook.xml found")},
		},
		duckdb_parser.RVToolsFailureMessage,
	),
)

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
		headers: []string{"Datacenter", "Cluster", "# Cores", "# CPU", "Object ID", "# Memory", "Model", "Vendor", "Host", "Config status", "VMotion support", "Storage VMotion support", "CPU Model", "Serial number", "Service tag"},
		row:     []string{"dc1", "cluster1", "8", "2", "host-1", "32768", "ESXi", "VMware", "esxi1", "green", "true", "true", " Xeon Gold 6338 ", " SN-1 ", "TAG-1"},
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
