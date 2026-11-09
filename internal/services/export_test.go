package services_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("Virtual disk export", func() {
	var (
		ctx  context.Context
		pool *store.Pool
		st   *store.Store
		svc  *services.ExportService
	)

	BeforeEach(func() {
		ctx = context.Background()
		pool = store.NewPool(5 * time.Minute)
		db, err := pool.NewDatabase("export", filepath.Join(GinkgoT().TempDir(), "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		DeferCleanup(pool.Close)
		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
			return migrations.RunCollection(ctx, sqlDB, "collection")
		})).To(Succeed())
		svc = services.NewExportService(st)
	})

	DescribeTable("exports disk inventory alongside overview", func(format string, populated bool) {
		Expect(svc.SupportedScopes()).To(ContainElement("disks"))
		Expect(svc.IsValidScope("disks")).To(BeTrue())
		Expect(svc.IsValidScope("bogus")).To(BeFalse())

		expected := [][]string{{
			"vm_name", "vm_id", "datacenter", "cluster", "host", "disk_key", "unit_number",
			"path", "disk_path", "capacity_mib", "sharing_mode", "raw", "shared_bus",
			"disk_mode", "disk_uuid", "thin", "controller", "label", "scsi_unit_number",
		}}
		if populated {
			_, err := st.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Datacenter", "Cluster", "Host", "Template", "migration_excluded")
				VALUES ('vm-1', '=inventory', 'dc', 'cluster', 'host', true, true),
				       ('vm-2', 'other-vm', NULL, NULL, NULL, false, false),
				       ('vm-3', 'no-disks', NULL, NULL, NULL, false, false);
				INSERT INTO vdisk ("VM ID", "Disk Key", "Unit #", "Path", "Disk Path", "Capacity MiB",
					"Sharing mode", "Raw", "Shared Bus", "Disk Mode", "Disk UUID", "Thin", "Controller", "Label", "SCSI Unit #")
				VALUES ('vm-1', '10', '1', '[ds] vm/disk.vmdk', 'rdm://disk-1', 102400,
					true, true, 'physicalSharing', 'independent_persistent', 'uuid-1', true, 'SCSI controller 0', 'Hard disk 2', '1'),
				       ('vm-1', '2', '0', '@disk', '[ds] vm/boot.vmdk', 2048,
					false, false, 'noSharing', 'persistent', 'uuid-2', false, 'SCSI controller 0', 'Hard disk 1', '0'),
				       ('vm-2', '2000', NULL, NULL, NULL, NULL,
					NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)
			`)
			Expect(err).NotTo(HaveOccurred())
			expected = append(expected,
				[]string{"'=inventory", "vm-1", "dc", "cluster", "host", "2", "0", "'@disk", "[ds] vm/boot.vmdk", "2048", "false", "false", "noSharing", "persistent", "uuid-2", "false", "SCSI controller 0", "Hard disk 1", "0"},
				[]string{"'=inventory", "vm-1", "dc", "cluster", "host", "10", "1", "[ds] vm/disk.vmdk", "rdm://disk-1", "102400", "true", "true", "physicalSharing", "independent_persistent", "uuid-1", "true", "SCSI controller 0", "Hard disk 2", "1"},
				[]string{"other-vm", "vm-2", "", "", "", "2000", "", "", "", "0", "false", "false", "", "", "", "false", "", "", ""},
			)
		}

		expected[0] = append(expected[0], "rdm_lun_uuid", "datastore_id", "datastore_name", "datastore_type",
			"datastore_filesystem_type", "datastore_filesystem_major_version", "datastore_protocol_type",
			"datastore_storage_protocol", "datastore_backing_devices", "datastore_vendor", "datastore_model")
		for i := 1; i < len(expected); i++ {
			expected[i] = append(expected[i], "", "", "", "", "", "", "", "", "", "", "")
		}

		var buf bytes.Buffer
		scopes := []string{"overview", "disks"}
		if format == "zip" {
			Expect(svc.WriteZip(ctx, scopes, &buf)).To(Succeed())
			archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())
			Expect(archive.File).To(HaveLen(2))
			file, err := archive.Open("disks.csv")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(file.Close)
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(Equal(expected))
			overview, err := archive.Open("overview.csv")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(overview.Close)
			overviewRows, err := csv.NewReader(overview).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(overviewRows[0]).To(ContainElement("virtual_disk_count"))
		} else {
			Expect(svc.WriteExcel(ctx, scopes, &buf)).To(Succeed())
			workbook, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
			Expect(workbook.GetSheetList()).To(Equal([]string{"Overview", "Virtual Disks"}))
			rows, err := workbook.GetRows("Virtual Disks")
			Expect(err).NotTo(HaveOccurred())
			// Excel omits trailing empty cells when reading rows.
			for i := range rows {
				for len(rows[i]) < len(expected[0]) {
					rows[i] = append(rows[i], "")
				}
			}
			Expect(rows).To(Equal(expected))
		}
	},
		Entry("CSV ZIP", "zip", true),
		Entry("Excel", "xlsx", true),
		Entry("empty CSV ZIP", "zip", false),
		Entry("empty Excel", "xlsx", false),
	)
})

var _ = Describe("Guest process export", func() {
	var (
		ctx  context.Context
		pool *store.Pool
		st   *store.Store
		svc  *services.ExportService
	)

	BeforeEach(func() {
		ctx = context.Background()
		pool = store.NewPool(5 * time.Minute)
		db, err := pool.NewDatabase("export", filepath.Join(GinkgoT().TempDir(), "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		DeferCleanup(pool.Close)
		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
			return migrations.RunCollection(ctx, sqlDB, "collection")
		})).To(Succeed())
		svc = services.NewExportService(st)
	})

	DescribeTable("exports guest processes alongside overview", func(format string, populated bool) {
		Expect(svc.SupportedScopes()).To(ContainElement("processes"))
		Expect(svc.IsValidScope("processes")).To(BeTrue())

		expected := [][]string{{"vm_name", "vm_id", "cluster", "process_name"}}
		if populated {
			_, err := st.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Cluster", "Template", "guest_apps")
				VALUES ('vm-1', '=inventory', 'cluster', false,
					'[{"name":"nginx","version":"1.25"},{"name":"=calc","version":""},{"name":""}]'),
				       ('vm-2', 'other-vm', NULL, false, '[{"name":"sshd","version":"9.0"}]'),
				       ('vm-3', 'empty-apps', 'cluster', false, '[]'),
				       ('vm-4', 'null-apps', 'cluster', false, NULL)
			`)
			Expect(err).NotTo(HaveOccurred())
			expected = append(expected,
				// vm_name sorts before letters because '=' precedes them; the leading
				// apostrophes come from CSV-injection sanitization.
				[]string{"'=inventory", "vm-1", "cluster", "'=calc"},
				[]string{"'=inventory", "vm-1", "cluster", "nginx"},
				[]string{"other-vm", "vm-2", "", "sshd"},
			)
		}

		var buf bytes.Buffer
		scopes := []string{"overview", "processes"}
		if format == "zip" {
			Expect(svc.WriteZip(ctx, scopes, &buf)).To(Succeed())
			archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())
			Expect(archive.File).To(HaveLen(2))
			file, err := archive.Open("processes.csv")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(file.Close)
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(Equal(expected))
		} else {
			Expect(svc.WriteExcel(ctx, scopes, &buf)).To(Succeed())
			workbook, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
			Expect(workbook.GetSheetList()).To(Equal([]string{"Overview", "Processes"}))
			rows, err := workbook.GetRows("Processes")
			Expect(err).NotTo(HaveOccurred())
			// Excel omits trailing empty cells when reading rows.
			for i := range rows {
				for len(rows[i]) < len(expected[0]) {
					rows[i] = append(rows[i], "")
				}
			}
			Expect(rows).To(Equal(expected))
		}
	},
		Entry("CSV ZIP", "zip", true),
		Entry("Excel", "xlsx", true),
		Entry("empty CSV ZIP", "zip", false),
		Entry("empty Excel", "xlsx", false),
	)
})

var _ = Describe("Host export", func() {
	var (
		ctx  context.Context
		pool *store.Pool
		db   *store.Database
		st   *store.Store
		svc  *services.ExportService
	)

	BeforeEach(func() {
		ctx = context.Background()
		pool = store.NewPool(5 * time.Minute)
		var err error
		db, err = pool.NewDatabase("host-export", filepath.Join(GinkgoT().TempDir(), "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		DeferCleanup(pool.Close)
		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
			return migrations.RunCollection(ctx, sqlDB, "collection")
		})).To(Succeed())
		svc = services.NewExportService(st)
	})

	DescribeTable("exports host CPU model and hardware serial", func(format string, legacyCollection bool) {
		if legacyCollection {
			_, err := st.Querier().ExecContext(ctx, `
				ALTER TABLE vhost DROP COLUMN "CPU Model";
				ALTER TABLE vhost DROP COLUMN "Serial Number";
				DELETE FROM collection_schema_migrations WHERE version = 4;
			`)
			Expect(err).NotTo(HaveOccurred())

			Expect(db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
				return migrations.RunCollection(ctx, sqlDB, "collection")
			})).To(Succeed())
		}

		_, err := st.Querier().ExecContext(ctx, `
			INSERT INTO vhost ("Datacenter", "Cluster", "# Cores", "# CPU", "# Memory", "Vendor", "Model", "Host", "Object ID", "Config status", "CPU Model", "Serial Number")
			VALUES ('dc-1', 'cluster-1', 32, 2, 262144, 'Dell', 'PowerEdge', 'esx-1', 'host-1', 'green', 'Xeon Gold 6338', 'SN-1'),
			       ('dc-1', 'cluster-1', 16, 1, 131072, 'HPE', 'ProLiant', 'esx-2', 'host-2', 'green', NULL, NULL);
			INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'vm-1', 'esx-1');
		`)
		Expect(err).NotTo(HaveOccurred())

		var output bytes.Buffer
		if format == "zip" {
			Expect(svc.WriteZip(ctx, []string{"hosts"}, &output)).To(Succeed())
		} else {
			Expect(svc.WriteExcel(ctx, []string{"hosts"}, &output)).To(Succeed())
		}

		var rows [][]string
		if format == "zip" {
			archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
			Expect(err).NotTo(HaveOccurred())
			Expect(archive.File).To(HaveLen(1))
			file, err := archive.File[0].Open()
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(file.Close)
			rows, err = csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
		} else {
			workbook, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
			Expect(workbook.GetSheetList()).To(Equal([]string{"Hosts"}))
			rows, err = workbook.GetRows("Hosts")
			Expect(err).NotTo(HaveOccurred())
		}

		header := []string{"id", "datacenter", "cluster", "cpu_sockets", "cpu_cores", "memory_mib", "memory_gib", "vendor", "model", "config_status", "vm_count", "cpu_model", "serial_number", "vmotion_supported", "storage_vmotion_supported", "vcenter_id"}
		Expect(rows).To(HaveLen(3))
		Expect(rows[0]).To(Equal(header))
		for i := range rows {
			for len(rows[i]) < len(header) {
				rows[i] = append(rows[i], "")
			}
		}
		Expect(rows[1][0:3]).To(Equal([]string{"host-1", "dc-1", "cluster-1"}))
		Expect(rows[1][3:5]).To(Equal([]string{"2", "32"}))
		Expect(rows[2][0:3]).To(Equal([]string{"host-2", "dc-1", "cluster-1"}))
		Expect(rows[1][11:13]).To(Equal([]string{"Xeon Gold 6338", "SN-1"}))
		Expect(rows[2][11:13]).To(Equal([]string{"", ""}))
	},
		Entry("CSV ZIP with current collection", "zip", false),
		Entry("Excel with current collection", "xlsx", false),
		Entry("CSV ZIP upgrades a collection without host hardware columns", "zip", true),
		Entry("Excel upgrades a collection without host hardware columns", "xlsx", true),
	)
})
