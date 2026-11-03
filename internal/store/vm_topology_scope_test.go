package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("VMStore storage topology", func() {
	var ctx context.Context
	var db *sql.DB
	var topology, ctes, query string

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = sql.Open("duckdb", filepath.Join(GinkgoT().TempDir(), "collection.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = db.Close() })
		Expect(duckdb_parser.New(db, nil).Init()).NotTo(HaveOccurred())
		Expect(migrations.RunCollection(ctx, db, "collection")).NotTo(HaveOccurred())
		_, err = db.Exec(`INSERT INTO vinfo ("VM ID", "VM", "Template") VALUES
('vm-1', 'target-vm', false), ('vm-2', 'other-vm', false), ('vm-empty', 'empty-vm', false);
INSERT INTO vhost ("Object ID", "Host", "Cluster") VALUES
('host-1', 'esxi1', 'cluster1'), ('host-2', 'esxi2', 'cluster1'), ('host-3', 'esxi3', 'cluster2');
INSERT INTO vhba ("Host ID", "Device", "Protocol") VALUES
('host-1', 'vmhba64', 'iSCSI'), ('host-2', 'vmhba64', 'FibreChannel'), ('host-3', 'vmhba64', 'FibreChannel');
INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor", "LUN Model") VALUES
('host-1', 'vmhba64', 'naa.shared', 'NETAPP', 'LUN-A'),
('host-2', 'vmhba64', 'naa.shared', 'PURE', 'LUN-B'),
('host-1', 'vmhba64', 'naa.second', 'DELL', 'LUN-C'),
('host-3', 'vmhba64', 'naa.shared', 'UNRELATED', 'UNRELATED');
INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices", "Type", "Storage Protocol") VALUES
('ds-shared', 'shared-ds', 'host-1,host-2', '["naa.shared"]', 'VMFS', NULL),
('ds-second', 'second-ds', 'host-1', '["naa.second"]', 'VMFS', NULL),
('ds-nfs', 'nfs-ds', 'host-1', '[]', 'NFS41', 'NFS');
INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices", "Type")
SELECT 'ds-unrelated-' || i, 'unrelated-' || i, 'host-3', '["naa.shared"]', 'VMFS' FROM range(1, 501) AS r(i);
INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Disk Path", "Capacity MiB") VALUES
('vm-1', '2000', '[shared-ds] vm/a.vmdk', NULL, 100),
('vm-1', '2001', NULL, '[shared-ds] vm/b.vmdk', 100),
('vm-1', '2002', '[second-ds] vm/c.vmdk', NULL, 100),
('vm-1', '2003', '[nfs-ds] vm/d.vmdk', NULL, 100),
('vm-1', '2004', '[missing-ds] vm/e.vmdk', NULL, 100),
('vm-1', '2005', NULL, NULL, 100),
('vm-2', '2000', '[unrelated-1] vm/other.vmdk', NULL, 100);`)
		Expect(err).NotTo(HaveOccurred())
		topology, err = duckdb_parser.VMStorageTopologyQuery(ctx, db)
		Expect(err).NotTo(HaveOccurred())
		// Inspect the production CTE results before the final VM join can hide
		// unnecessary topology aggregation for unrelated datastores.
		var ok bool
		ctes, _, ok = strings.Cut(vmGetQuery, "\nSELECT\n")
		Expect(ok).To(BeTrue())
		query = "WITH " + topology + ", " + ctes + `
SELECT count(*), CAST(COALESCE(to_json(list(ds."Object ID" ORDER BY ds."Object ID")), '[]') AS VARCHAR),
       (SELECT count(*) FROM storage_disk_datastores)
FROM backing_info JOIN storage_datastores ds USING (datastore_row_id)`
	})

	DescribeTable("aggregates only the requested VM's datastore topology", func(vmID, datastoreIDs string, expectedGroups, expectedResolvedDisks int) {
		var got string
		var groups, resolvedDisks int
		Expect(db.QueryRowContext(ctx, query, vmID, vmID).Scan(&groups, &got, &resolvedDisks)).NotTo(HaveOccurred())
		Expect(groups).To(Equal(expectedGroups))
		Expect(got).To(MatchJSON(datastoreIDs))
		Expect(resolvedDisks).To(Equal(expectedResolvedDisks), "datastore resolution must exclude other VMs' disks")
	},
		Entry("target VM", "vm-1", `["ds-second", "ds-shared"]`, 2, 4),
		Entry("other VM", "vm-2", `["ds-unrelated-1"]`, 1, 1),
		Entry("VM without disks", "vm-empty", `[]`, 0, 0),
		Entry("missing VM", "missing-vm", `[]`, 0, 0),
	)

	It("does not multiply backing paths for disks sharing a datastore", func() {
		var paths int
		Expect(db.QueryRowContext(ctx, "WITH "+topology+", "+ctes+` SELECT count(*) FROM backing_paths`, "vm-1", "vm-1").Scan(&paths)).NotTo(HaveOccurred())
		Expect(paths).To(Equal(3), "two disks on one datastore must not multiply its backing paths")
	})

	It("preserves all disks and their resolved metadata in VM details", func() {
		vm, err := NewVMStore(db).Get(ctx, "vm-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.Disks).To(HaveLen(6), "unresolved datastores and missing disk paths must not remove disks")
		for _, disk := range vm.Disks {
			Expect(disk.Capacity).To(Equal(int64(100)))
			switch disk.File {
			case "[shared-ds] vm/a.vmdk", "[shared-ds] vm/b.vmdk":
				Expect(disk.Datastore).NotTo(BeNil())
				Expect(disk.Datastore.ProtocolType).To(Equal("FibreChannel, iSCSI"))
				Expect(disk.Datastore.Vendor).To(Equal("NETAPP, PURE"))
				Expect(disk.Datastore.Model).To(Equal("LUN-A, LUN-B"))
			case "[second-ds] vm/c.vmdk":
				Expect(disk.Datastore).NotTo(BeNil())
				Expect(disk.Datastore.ProtocolType).To(Equal("iSCSI"))
				Expect(disk.Datastore.Vendor).To(Equal("DELL"))
			case "[nfs-ds] vm/d.vmdk":
				Expect(disk.Datastore).NotTo(BeNil())
				Expect(disk.Datastore.ProtocolType).To(Equal("NFS"))
				Expect(disk.Datastore.StorageProtocol).To(Equal("File"))
			case "[missing-ds] vm/e.vmdk", "":
				Expect(disk.Datastore).To(BeNil())
			default:
				Fail("unexpected disk: " + disk.File)
			}
		}
	})
})
