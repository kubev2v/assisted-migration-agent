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

		var buf bytes.Buffer
		scopes := []string{"overview", "disks"}
		if format == "zip" {
			Expect(svc.WriteZip(ctx, scopes, &buf)).To(Succeed())
			archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())
			Expect(archive.File).To(HaveLen(2))
			file, err := archive.Open("disks.csv")
			Expect(err).NotTo(HaveOccurred())
			defer file.Close()
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(Equal(expected))
			overview, err := archive.Open("overview.csv")
			Expect(err).NotTo(HaveOccurred())
			defer overview.Close()
			overviewRows, err := csv.NewReader(overview).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(overviewRows[0]).To(ContainElement("virtual_disk_count"))
		} else {
			Expect(svc.WriteExcel(ctx, scopes, &buf)).To(Succeed())
			workbook, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			defer workbook.Close()
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
