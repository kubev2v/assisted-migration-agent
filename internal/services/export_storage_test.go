package services_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("Storage export", func() {
	var (
		ctx context.Context
		st  *store.Store
		db  *store.Database
		svc *services.ExportService
	)

	BeforeEach(func() {
		ctx = context.Background()

		pool := store.NewPool(5 * time.Minute)
		dir := GinkgoT().TempDir()
		extension := os.Getenv("DUCKDB_TEST_EXTENSION")
		if extension != "" {
			Expect(os.Symlink(extension, filepath.Join(dir, "sqlite_scanner.duckdb_extension"))).To(Succeed())
		}

		var err error
		db, err = pool.NewDatabase("export", filepath.Join(dir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
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

	DescribeTable("extends existing host and datastore exports", func(format string) {
		const seed = `
			INSERT INTO vhost ("Object ID", "Host", "CPU Model", "Serial Number", "VMotion support", "Storage VMotion support")
				VALUES ('host-1', 'esxi-1', 'Xeon Gold 6338', 'SN-1', true, false),
				       ('host-2', 'esxi-2', NULL, NULL, NULL, NULL),
				       ('duplicate', 'ambiguous-a', NULL, NULL, false, false),
				       ('duplicate', 'ambiguous-b', NULL, NULL, false, false);
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Capacity MiB", "Free MiB", "Storage Protocol", "Filesystem Type", "Filesystem Major Version", "Backing Devices", "SIOC Enabled")
                VALUES ('ds-1', 'data', '["host-1","esxi-2","duplicate","missing"]', 2048, 512, 'FC', 'VMFS', 6, '["naa.1"]', true),
                       ('ds-2', 'empty', 'host-1', 0, 0, NULL, NULL, NULL, '[]', NULL),
                       ('ds-3', 'tiny', '', 1, 0, NULL, NULL, NULL, NULL, NULL),
                       ('ds-4', 'unknown', '', NULL, NULL, NULL, NULL, NULL, NULL, NULL);`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		scopes := []string{"overview", "disks", "datastores", "hbas", "luns", "hosts"}
		var buf bytes.Buffer
		var datastoreRows, hostRows [][]string

		if format == "zip" {
			Expect(svc.WriteZip(ctx, scopes, &buf)).To(Succeed())

			archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())
			Expect(archive.File).To(HaveLen(6))

			file, err := archive.Open("datastores.csv")
			Expect(err).NotTo(HaveOccurred())
			datastoreRows, err = csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())

			file, err = archive.Open("hosts.csv")
			Expect(err).NotTo(HaveOccurred())
			hostRows, err = csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())
		} else {
			Expect(svc.WriteExcel(ctx, scopes, &buf)).To(Succeed())

			workbook, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
			Expect(workbook.GetSheetList()).To(Equal([]string{"Overview", "Virtual Disks", "Datastores", "Host Storage Adapters", "Host Storage LUNs", "Hosts"}))

			datastoreRows, err = workbook.GetRows("Datastores")
			Expect(err).NotTo(HaveOccurred())
			hostRows, err = workbook.GetRows("Hosts")
			Expect(err).NotTo(HaveOccurred())

			// Excel omits trailing empty cells when reading rows.
			for i := range datastoreRows {
				for len(datastoreRows[i]) < len(datastoreRows[0]) {
					datastoreRows[i] = append(datastoreRows[i], "")
				}
			}
			for i := range hostRows {
				for len(hostRows[i]) < len(hostRows[0]) {
					hostRows[i] = append(hostRows[i], "")
				}
			}
		}

		Expect(datastoreRows[0]).To(Equal([]string{"name", "id", "address", "type", "capacity_mib", "capacity_gib", "free_mib", "free_gib", "used_percent", "mha_enabled", "hosts", "vm_count", "used_gib", "storage_protocol", "filesystem_type", "filesystem_major_version", "backing_devices", "sioc_enabled", "protocol_type", "storage_access_category", "vendor", "model"}))
		Expect(datastoreRows[0]).NotTo(ContainElement("host_names"))
		Expect(datastoreRows[1][10]).To(Equal(`["host-1","esxi-2","duplicate","missing"]`))
		Expect(datastoreRows[1][12:]).To(Equal([]string{"1.5", "FC", "VMFS", "6", `["naa.1"]`, "true", "FibreChannel", "", "", ""}))
		Expect(datastoreRows[2][8]).To(Equal("0.0"))
		Expect(datastoreRows[3][12]).To(Equal("0.0009765625"))
		Expect(datastoreRows[4][12]).To(BeEmpty())

		Expect(hostRows[0]).To(Equal([]string{"id", "datacenter", "cluster", "cpu_sockets", "cpu_cores", "memory_mib", "memory_gib", "vendor", "model", "config_status", "vm_count", "cpu_model", "serial_number", "vmotion_supported", "storage_vmotion_supported", "vcenter_id"}))
		Expect(hostRows[0]).NotTo(ContainElement("name"))
		Expect(hostRows[3][11:13]).To(Equal([]string{"Xeon Gold 6338", "SN-1"}))
		Expect(hostRows[3][13:]).To(Equal([]string{"true", "false", ""}))
		Expect(hostRows[4][11:]).To(Equal([]string{"", "", "", "", ""}))
	},
		Entry("CSV ZIP", "zip"),
		Entry("Excel", "xlsx"),
	)

	DescribeTable("exports complete VM datastore, RDM, and adapter metadata", func(format string) {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host", "VI SDK UUID") VALUES ('host-1', 'esxi-1', 'vc-1');
            INSERT INTO vhba ("Host ID", "Device", "HBA Key", "Protocol") VALUES (' host-1 ', 'vmhba1', 'key-1', 'FC');
            INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor", "LUN Model")
                VALUES (' host-1 ', 'vmhba1', 'naa.1', 'vendor', 'model');
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Type", "Filesystem Type", "Filesystem Major Version", "Backing Devices")
                VALUES ('ds-1', 'data', '["host-1"]', 'VMFS', 'VMFS', 6, '["naa.1"]');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'test-vm', 'esxi-1');
            INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Raw", "RDM LUN UUID") VALUES
                ('vm-1', '1', '[data] vm/rdm', true, 'lun-uuid'),
                ('vm-1', '2', '[data] vm/file', false, 'not-rdm');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		var archive *zip.Reader
		var workbook *excelize.File

		if format == "zip" {
			Expect(svc.WriteZip(ctx, []string{"disks", "datastores", "hosts", "hbas"}, &buf)).To(Succeed())
			archive, err = zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(svc.WriteExcel(ctx, []string{"disks", "datastores", "hosts", "hbas"}, &buf)).To(Succeed())
			workbook, err = excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
		}

		expected := map[string]map[string]string{
			"datastores.csv": {"protocol_type": "FibreChannel", "storage_access_category": "Block", "vendor": "vendor", "model": "model"},
			"hosts.csv":      {"vcenter_id": "vc-1"},
			"hbas.csv":       {"adapter_key": "key-1"},
			"disks.csv":      {"rdm_lun_uuid": "lun-uuid", "datastore_id": "ds-1", "datastore_name": "data", "datastore_type": "VMFS", "datastore_filesystem_type": "VMFS", "datastore_filesystem_major_version": "6", "datastore_protocol_type": "FibreChannel", "datastore_storage_protocol": "Block", "datastore_backing_devices": `["naa.1"]`, "datastore_vendor": "vendor", "datastore_model": "model"},
		}

		for filename, columns := range expected {
			var rows [][]string

			if format == "zip" {
				file, err := archive.Open(filename)
				Expect(err).NotTo(HaveOccurred())
				rows, err = csv.NewReader(file).ReadAll()
				Expect(err).NotTo(HaveOccurred())
				Expect(file.Close()).To(Succeed())
			} else {
				sheets := map[string]string{"datastores.csv": "Datastores", "hosts.csv": "Hosts", "hbas.csv": "Host Storage Adapters", "disks.csv": "Virtual Disks"}
				rows, err = workbook.GetRows(sheets[filename])
				Expect(err).NotTo(HaveOccurred())

				// Excel omits trailing empty cells when reading rows.
				for i := range rows {
					for len(rows[i]) < len(rows[0]) {
						rows[i] = append(rows[i], "")
					}
				}
			}

			Expect(len(rows)).To(BeNumerically(">=", 2))

			for column, want := range columns {
				index := slices.Index(rows[0], column)
				Expect(index).NotTo(Equal(-1), filename+" missing "+column)
				Expect(rows[1][index]).To(Equal(want), filename+" "+column)
			}

			if filename == "disks.csv" {
				index := slices.Index(rows[0], "rdm_lun_uuid")
				Expect(rows[2][index]).To(BeEmpty())
			}
		}
	},
		Entry("CSV ZIP", "zip"),
		Entry("Excel", "xlsx"),
	)

	DescribeTable("classifies datastore access categories", func(dsType, category string) {
		_, err := st.Querier().ExecContext(ctx, `INSERT INTO vdatastore ("Object ID", "Name", "Type") VALUES ('ds', 'data', ?)`, dsType)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"datastores"}, &buf)).To(Succeed())

		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		file, err := archive.Open("datastores.csv")
		Expect(err).NotTo(HaveOccurred())
		rows, err := csv.NewReader(file).ReadAll()
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Close()).To(Succeed())

		index := slices.Index(rows[0], "storage_access_category")
		Expect(index).NotTo(Equal(-1))
		Expect(rows[1][index]).To(Equal(category))
		Expect(rows[1][index]).To(Equal(inventory.StorageProtocolFromDatastoreType(dsType)))
	},
		Entry("VMFS", "VMFS", "Block"),
		Entry("NFS", "NFS", "File"),
		Entry("NFS41", "NFS41", "File"),
		Entry("NFS4.1", "NFS4.1", "File"),
		Entry("NFS 4.1", " nfs 4.1 ", "File"),
		Entry("vSAN", "VSAN", "Object"),
		Entry("unknown", "VVOL", ""),
		Entry("empty", "", ""),
	)

	It("resolves duplicate datastore names per VM without guessing ambiguous matches", func() {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host") VALUES ('host-1', 'esxi-1'), ('host-2', 'esxi-2');
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Type", "Backing Devices") VALUES
                ('ds-1', 'shared', '["host-1"]', 'VMFS', '["naa.1"]'),
                ('ds-2', 'shared', 'host-2', 'NFS', '["naa.2"]');
            INSERT INTO vhostscsi ("Host ID", "Canonical Name", "LUN Vendor", "LUN Model") VALUES
                ('host-1', 'naa.1', 'vendor-1', 'model-1'), ('host-2', 'naa.2', 'vendor-2', 'model-2');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES
                ('vm-1', 'vm-1', 'esxi-1'), ('vm-2', 'vm-2', 'esxi-2'), ('vm-3', 'vm-3', 'unknown');
            INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Disk Path", "Raw", "RDM LUN UUID") VALUES
                ('vm-1', '1', NULL, '[shared] vm/one', true, 'lun-1'),
                ('vm-2', '1', '[shared] vm/two', NULL, false, NULL),
                ('vm-3', '1', '[shared] vm/three', NULL, true, 'lun-3');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"disks"}, &buf)).To(Succeed())

		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		file, err := archive.Open("disks.csv")
		Expect(err).NotTo(HaveOccurred())
		rows, err := csv.NewReader(file).ReadAll()
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Close()).To(Succeed())
		Expect(rows).To(HaveLen(4))

		type expectedDisk struct {
			id, ds, category, vendor, model, rdm string
		}

		expected := []expectedDisk{
			{"vm-1", "ds-1", "Block", "vendor-1", "model-1", "lun-1"},
			{"vm-2", "ds-2", "File", "vendor-2", "model-2", ""},
			{"vm-3", "", "", "", "", "lun-3"},
		}

		for i, want := range expected {
			columns := map[string]string{
				"vm_id":                      want.id,
				"datastore_id":               want.ds,
				"datastore_storage_protocol": want.category,
				"datastore_vendor":           want.vendor,
				"datastore_model":            want.model,
				"rdm_lun_uuid":               want.rdm,
			}

			for column, value := range columns {
				index := slices.Index(rows[0], column)
				Expect(index).NotTo(Equal(-1))
				Expect(rows[i+1][index]).To(Equal(value), column)
			}
		}
	})

	DescribeTable("exports adapters and LUNs with unresolved owners", func(format string) {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host") VALUES ('host-1', 'esxi-1'), ('duplicate', 'esxi-2'), (' duplicate ', 'esxi-3');
            INSERT INTO vhba ("Host ID", "Device", "Protocol", "Node WWN", "Port WWN") VALUES
                ('host-1', 'vmhba1', 'FC', '001234', '000567'), ('duplicate', 'vmhba2', 'iSCSI', NULL, NULL);
            INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor") VALUES
                (' host-1 ', 'vmhba1', 'naa.1', 'vendor'), ('missing', NULL, 'naa.2', 'vendor');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		expected := map[string][][]string{
			"hbas": {{"host_id", "host", "device", "adapter_type", "protocol", "model", "driver", "node_wwn", "port_wwn", "adapter_key"},
				{"duplicate", "", "vmhba2", "", "iSCSI", "", "", "", "", ""}, {"host-1", "esxi-1", "vmhba1", "", "FC", "", "", "001234", "000567", ""}},
			"luns": {{"host_id", "host", "hba_device", "canonical_name", "lun_vendor", "lun_model", "lun_uuid", "scsi_disk_key"},
				{" host-1 ", "esxi-1", "vmhba1", "naa.1", "vendor", "", "", ""}, {"missing", "", "", "naa.2", "vendor", "", "", ""}},
		}

		var buf bytes.Buffer

		if format == "zip" {
			Expect(svc.WriteZip(ctx, []string{"hbas", "luns"}, &buf)).To(Succeed())

			archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			Expect(err).NotTo(HaveOccurred())

			for scope, want := range expected {
				file, err := archive.Open(scope + ".csv")
				Expect(err).NotTo(HaveOccurred())
				rows, err := csv.NewReader(file).ReadAll()
				Expect(err).NotTo(HaveOccurred())
				Expect(file.Close()).To(Succeed())
				Expect(rows).To(Equal(want))
			}
		} else {
			Expect(svc.WriteExcel(ctx, []string{"hbas", "luns"}, &buf)).To(Succeed())

			workbook, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)

			sheets := map[string]string{"hbas": "Host Storage Adapters", "luns": "Host Storage LUNs"}
			for scope, sheet := range sheets {
				rows, err := workbook.GetRows(sheet)
				Expect(err).NotTo(HaveOccurred())

				// Excel omits trailing empty cells when reading rows.
				for i := range rows {
					for len(rows[i]) < len(expected[scope][0]) {
						rows[i] = append(rows[i], "")
					}
				}
				Expect(rows).To(Equal(expected[scope]))
			}

			cellType, err := workbook.GetCellType("Host Storage Adapters", "H3")
			Expect(err).NotTo(HaveOccurred())
			Expect(cellType).To(Equal(excelize.CellTypeSharedString))
		}
	},
		Entry("CSV ZIP", "zip"),
		Entry("Excel", "xlsx"),
	)

	DescribeTable("matches non-empty adapter devices after trimming", func(adapter, device, protocol string) {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host") VALUES ('host-1', 'esxi-1');
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices") VALUES ('ds-1', 'data', 'host-1', '["naa.1"]');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'vm', 'esxi-1');
            INSERT INTO vdisk ("VM ID", "Disk Key", "Path") VALUES ('vm-1', '1', '[data] vm/disk');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vhba ("Host ID", "Device", "Protocol") VALUES ('host-1', ?, 'FC')`, adapter)
		Expect(err).NotTo(HaveOccurred())
		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name") VALUES ('host-1', ?, 'naa.1')`, device)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"datastores", "disks"}, &buf)).To(Succeed())

		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		columns := map[string]string{"datastores.csv": "protocol_type", "disks.csv": "datastore_protocol_type"}
		for filename, column := range columns {
			file, err := archive.Open(filename)
			Expect(err).NotTo(HaveOccurred())
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())
			index := slices.Index(rows[0], column)
			Expect(index).NotTo(Equal(-1))
			Expect(rows[1][index]).To(Equal(protocol), filename)
		}
	},
		Entry("padded devices", " vmhba1 ", "vmhba1 ", "FibreChannel"),
		Entry("blank devices do not match", "  ", "  ", ""),
		Entry("unmodified devices", "vmhba1", "vmhba1", "FibreChannel"),
	)

	DescribeTable("does not match blank backing device identities", func(backingDevices, canonicalName, protocol, vendor, model string) {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host") VALUES ('host-1', 'esxi-1');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'vm', 'esxi-1');
            INSERT INTO vdisk ("VM ID", "Disk Key", "Path") VALUES ('vm-1', '1', '[data] vm/disk');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices") VALUES ('ds-1', 'data', 'host-1', ?)`, backingDevices)
		Expect(err).NotTo(HaveOccurred())
		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vhba ("Host ID", "Device", "Protocol") VALUES ('host-1', 'vmhba1', 'FC')`)
		Expect(err).NotTo(HaveOccurred())
		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor", "LUN Model") VALUES ('host-1', 'vmhba1', ?, 'vendor', 'model')`, canonicalName)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"datastores", "disks"}, &buf)).To(Succeed())
		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		for filename, expected := range map[string]map[string]string{
			"datastores.csv": {
				"protocol_type": protocol,
				"vendor":        vendor,
				"model":         model,
			},
			"disks.csv": {
				"datastore_protocol_type": protocol,
				"datastore_vendor":        vendor,
				"datastore_model":         model,
			},
		} {
			file, err := archive.Open(filename)
			Expect(err).NotTo(HaveOccurred())
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())

			for column, want := range expected {
				index := slices.Index(rows[0], column)
				Expect(index).NotTo(Equal(-1), filename+" missing "+column)
				Expect(rows[1][index]).To(Equal(want), filename+" "+column)
			}
		}
	},
		Entry("blank backing and canonical names", `[""]`, "", "", "", ""),
		Entry("blank backing with named canonical name", `[""]`, "naa.1", "", "", ""),
		Entry("named backing with blank canonical name", `["naa.1"]`, "", "", "", ""),
		Entry("matching nonblank names", `["naa.1"]`, "naa.1", "FibreChannel", "vendor", "model"),
	)

	DescribeTable("uses disk paths consistently for disk and datastore exports", func(path any, diskPath, datastoreID, vmCount string) {
		const seed = `
            INSERT INTO vhost ("Object ID", "Host") VALUES ('host-1', 'esxi-1');
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts") VALUES ('ds-1', 'data', '["host-1"]');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'vm', 'esxi-1');`

		_, err := st.Querier().ExecContext(ctx, seed)
		Expect(err).NotTo(HaveOccurred())

		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Disk Path") VALUES ('vm-1', '1', ?, ?)`, path, diskPath)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"datastores", "disks"}, &buf)).To(Succeed())

		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		expected := map[string]map[string]string{
			"datastores.csv": {"vm_count": vmCount},
			"disks.csv":      {"datastore_id": datastoreID},
		}

		for filename, columns := range expected {
			file, err := archive.Open(filename)
			Expect(err).NotTo(HaveOccurred())
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())

			for column, want := range columns {
				index := slices.Index(rows[0], column)
				Expect(index).NotTo(Equal(-1))
				Expect(rows[1][index]).To(Equal(want), filename+" "+column)
			}
		}
	},
		Entry("Path @disk falls back to Disk Path", "@disk", "[data] vm/disk", "ds-1", "1"),
		Entry("empty Path falls back to Disk Path", "", "[data] vm/disk", "ds-1", "1"),
		Entry("parseable path takes precedence", "[other] vm/disk", "[data] vm/disk", "", "0"),
		Entry("bracket inside Path falls back to Disk Path", "disk[1].vmdk", "[data] vm/disk", "ds-1", "1"),
		Entry("no datastore name", "@disk", "plain", "", "0"),
		Entry("NULL Path uses Disk Path", nil, "[data] vm/disk", "ds-1", "1"),
	)

	It("reads storage tables and columns regardless of identifier casing", func() {
		const recasedSchema = `
            DROP TABLE vhba;
            CREATE TABLE "VHBA" ("HOST ID" VARCHAR, "DEVICE" VARCHAR, "TYPE" VARCHAR, "HBA KEY" VARCHAR,
                "PROTOCOL" VARCHAR, "MODEL" VARCHAR, "DRIVER" VARCHAR, "NODE WWN" VARCHAR, "PORT WWN" VARCHAR);
            INSERT INTO "VHBA" VALUES ('host-1', 'vmhba1', NULL, NULL, 'FC', NULL, NULL, NULL, NULL);
            DROP TABLE vhostscsi;
            CREATE TABLE "VHOSTSCSI" ("HOST ID" VARCHAR, "HBA DEVICE" VARCHAR, "SCSI DISK KEY" VARCHAR,
                "CANONICAL NAME" VARCHAR, "LUN VENDOR" VARCHAR, "LUN MODEL" VARCHAR, "LUN UUID" VARCHAR);
            INSERT INTO "VHOSTSCSI" VALUES ('host-1', 'vmhba1', NULL, 'naa.1', 'vendor', NULL, NULL);
            ALTER TABLE vhost DROP COLUMN "VI SDK UUID";
            ALTER TABLE vhost ADD COLUMN "vi sdk uuid" VARCHAR;
            ALTER TABLE vdatastore DROP COLUMN "Filesystem Type";
            ALTER TABLE vdatastore ADD COLUMN "filesystem type" VARCHAR;
            ALTER TABLE vdisk DROP COLUMN "RDM LUN UUID";
            ALTER TABLE vdisk ADD COLUMN "rdm lun uuid" VARCHAR;
            INSERT INTO vhost ("Object ID", "Host", "vi sdk uuid") VALUES ('host-1', 'esxi-1', 'vc-1');
            INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "filesystem type", "Backing Devices")
                VALUES ('ds-1', 'data', 'host-1', 'VMFS', '["naa.1"]');
            INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES ('vm-1', 'vm', 'esxi-1');
            INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Raw", "rdm lun uuid")
                VALUES ('vm-1', '1', '[data] vm/disk', true, 'lun-1');`

		_, err := st.Querier().ExecContext(ctx, recasedSchema)
		Expect(err).NotTo(HaveOccurred())

		var buf bytes.Buffer
		Expect(svc.WriteZip(ctx, []string{"hbas", "luns", "hosts", "datastores", "disks"}, &buf)).To(Succeed())

		archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		Expect(err).NotTo(HaveOccurred())

		expected := map[string]map[string]string{
			"hbas.csv":       {"host_id": "host-1", "device": "vmhba1", "protocol": "FC"},
			"luns.csv":       {"host_id": "host-1", "canonical_name": "naa.1", "lun_vendor": "vendor"},
			"hosts.csv":      {"vcenter_id": "vc-1"},
			"datastores.csv": {"filesystem_type": "VMFS", "protocol_type": "FibreChannel", "vendor": "vendor"},
			"disks.csv":      {"rdm_lun_uuid": "lun-1", "datastore_filesystem_type": "VMFS"},
		}

		for filename, columns := range expected {
			file, err := archive.Open(filename)
			Expect(err).NotTo(HaveOccurred())
			rows, err := csv.NewReader(file).ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(file.Close()).To(Succeed())
			Expect(rows).To(HaveLen(2), filename)

			for column, want := range columns {
				index := slices.Index(rows[0], column)
				Expect(index).NotTo(Equal(-1))
				Expect(rows[1][index]).To(Equal(want), filename+" "+column)
			}
		}
	})
})
