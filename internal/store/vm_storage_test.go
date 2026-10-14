package store_test

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("VMStore storage metadata", func() {
	var (
		ctx  context.Context
		path string
		pool *store.Pool
		st   *store.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		path = filepath.Join(GinkgoT().TempDir(), "collection.duckdb")
		pool = store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
	})

	Context("current collections", func() {
		BeforeEach(func() {
			database, err := pool.NewDatabase("collection", path, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(database)
			Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				if err := duckdb_parser.New(db, nil).Init(); err != nil {
					return err
				}
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
			st, err = database.Store()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("RDM LUN UUID", func() {
			BeforeEach(func() {
				_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID","VM","Template") VALUES ('vm-1','rdm-vm',false);
  INSERT INTO vdatastore ("Object ID","Name","Type","Filesystem Type","Filesystem Major Version","Backing Devices") VALUES ('ds-1','shared-ds','VMFS','VMFS',6,'["naa.datastore-extent"]');
  INSERT INTO vdisk ("VM ID","Disk Key","Path","Raw","Capacity MiB","RDM LUN UUID") VALUES
  ('vm-1','2000','[shared-ds] vm/a.vmdk',true,100,'lun-uuid-A'),
  ('vm-1','2001','[shared-ds] vm/b.vmdk',true,200,'lun-uuid-B'),
  ('vm-1','2002','[shared-ds] vm/c.vmdk',false,300,'must-not-leak'),
  ('vm-1','2003','[shared-ds] vm/d.vmdk',true,400,NULL);`)
				Expect(err).NotTo(HaveOccurred())
			})
			It("preserves per-disk RDM LUN UUIDs and datastore metadata", func() {
				vm, err := st.VM().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				data, err := json.Marshal(v2.NewVirtualMachineDetailFromModel(*vm))
				Expect(err).NotTo(HaveOccurred())
				var got struct {
					Disks []map[string]any `json:"disks"`
				}
				Expect(json.Unmarshal(data, &got)).NotTo(HaveOccurred())
				Expect(got.Disks).To(HaveLen(4))
				byFile := map[string]map[string]any{}
				for _, d := range got.Disks {
					byFile[d["file"].(string)] = d
					Expect(d).NotTo(HaveKey("key"), "this feature does not change disk.key exposure")
				}
				Expect(byFile["[shared-ds] vm/a.vmdk"]["rdmBacking"]).To(Equal(map[string]any{"lunUuid": "lun-uuid-A"}))
				Expect(byFile["[shared-ds] vm/b.vmdk"]["rdmBacking"]).To(Equal(map[string]any{"lunUuid": "lun-uuid-B"}))
				Expect(byFile["[shared-ds] vm/c.vmdk"]).NotTo(HaveKey("rdmBacking"))
				Expect(byFile["[shared-ds] vm/d.vmdk"]).NotTo(HaveKey("rdmBacking"))
				Expect(byFile["[shared-ds] vm/c.vmdk"]["rdm"]).To(Equal(false))
				Expect(byFile["[shared-ds] vm/d.vmdk"]["rdm"]).To(Equal(true))
				Expect(byFile["[shared-ds] vm/a.vmdk"]["capacity"]).To(Equal(float64(100 * 1024 * 1024)))
				ds := byFile["[shared-ds] vm/a.vmdk"]["datastore"].(map[string]any)
				Expect(ds["name"]).To(Equal("shared-ds"))
				Expect(ds["filesystemMajorVersion"]).To(Equal(float64(6)))
				Expect(ds["backingDeviceIds"]).To(Equal([]any{"naa.datastore-extent"}))
			})
		})

		Context("NFS datastore protocol", func() {
			BeforeEach(func() {
				_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "Template") VALUES ('vm-1', 'nfs-vm', false);
INSERT INTO vdatastore ("Object ID", "Name", "Type", "Filesystem Type", "Storage Protocol")
VALUES ('ds-1', 'nfs-ds', 'NFS41', 'NFS41', 'NFS');
INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Capacity MiB")
VALUES ('vm-1', '2000', '[nfs-ds] vm/disk.vmdk', 100);`)
				Expect(err).NotTo(HaveOccurred())
			})
			DescribeTable("classifies NFS aliases as File and preserves the reported transport", func(datastoreType string) {
				_, err := st.Querier().ExecContext(ctx, `UPDATE vdatastore SET "Type" = ?`, datastoreType)
				Expect(err).NotTo(HaveOccurred())
				vm, err := st.VM().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(1))
				Expect(vm.Disks[0].Datastore).NotTo(BeNil())
				Expect(vm.Disks[0].Datastore.StorageProtocol).To(Equal("File"))
				detail := v2.NewVirtualMachineDetailFromModel(*vm)
				Expect(detail.Disks).To(HaveLen(1))
				ds := detail.Disks[0].Datastore
				Expect(ds).NotTo(BeNil())
				Expect(ds.StorageProtocol).NotTo(BeNil())
				Expect(*ds.StorageProtocol).To(Equal("File"))
				Expect(ds.Type).NotTo(BeNil())
				Expect(*ds.Type).To(Equal(datastoreType))
				Expect(ds.FilesystemType).NotTo(BeNil())
				Expect(*ds.FilesystemType).To(Equal("NFS41"))
				Expect(ds.ProtocolType).NotTo(BeNil())
				Expect(*ds.ProtocolType).To(Equal("NFS"))
			},
				Entry("NFS41", "NFS41"),
				Entry("NFS4.1", "NFS4.1"),
				Entry("NFS 4.1", "NFS 4.1"),
				Entry("whitespace and case variations", "  nfs 4.1  "),
			)
		})

		Context("host references", func() {
			DescribeTable("resolves only unambiguous host references", func(members, host2Name, protocol, vendor string) {
				_, err := st.Querier().ExecContext(ctx, `INSERT INTO vhost ("Object ID", "Host", "Cluster") VALUES ('host-1', 'esxi1.example.com', 'cluster1'), ('host-2', ?, 'cluster2')`, host2Name)
				Expect(err).NotTo(HaveOccurred())
				_, err = st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "Template") VALUES ('vm-1', 'vm1', false);
INSERT INTO vhba ("Host ID", "Device", "Protocol") VALUES ('host-1', 'vmhba64', 'iSCSI'), ('host-2', 'vmhba64', 'FibreChannel');
INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor", "LUN Model") VALUES
('host-1', 'vmhba64', 'naa.shared', 'NETAPP', 'LUN-A'), ('host-2', 'vmhba64', 'naa.shared', 'PURE', 'LUN-B');
INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Raw", "Capacity MiB", "RDM LUN UUID") VALUES ('vm-1', '2000', '[datastore1] vm/disk.vmdk', true, 100, 'rdm-lun-uuid');`)
				Expect(err).NotTo(HaveOccurred())
				_, err = st.Querier().ExecContext(ctx, `INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices", "Type") VALUES ('ds-1', 'datastore1', ?, '["naa.shared"]', 'VMFS')`, members)
				Expect(err).NotTo(HaveOccurred())
				vm, err := st.VM().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(1))
				disk := vm.Disks[0]
				Expect(disk.Capacity).To(Equal(int64(100)))
				Expect(disk.RDMBacking).NotTo(BeNil())
				Expect(disk.RDMBacking.LunUUID).To(Equal("rdm-lun-uuid"))
				Expect(disk.Datastore).NotTo(BeNil())
				Expect(disk.Datastore.Name).To(Equal("datastore1"))
				Expect(disk.Datastore.BackingDeviceIDs).To(Equal([]string{"naa.shared"}))
				Expect(disk.Datastore.ProtocolType).To(Equal(protocol))
				Expect(disk.Datastore.Vendor).To(Equal(vendor))
				count, err := st.VM().Count(ctx, store.ByFilter("datastore.name = 'datastore1'"))
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(1), "the datastore filter must still find the VM")
				var stored string
				Expect(st.Querier().QueryRowContext(ctx, `SELECT "Hosts" FROM vdatastore`).Scan(&stored)).NotTo(HaveOccurred())
				Expect(stored).To(Equal(members))
				csvPath := filepath.Join(GinkgoT().TempDir(), "datastores.csv")
				Expect(st.Export().CopyScope(ctx, "datastores", csvPath)).NotTo(HaveOccurred())
				data, err := os.ReadFile(csvPath)
				Expect(err).NotTo(HaveOccurred())
				records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
				Expect(err).NotTo(HaveOccurred())
				Expect(records).To(HaveLen(2))
				hostsColumn := slices.Index(records[0], "hosts")
				Expect(hostsColumn).NotTo(Equal(-1))
				Expect(records[1][hostsColumn]).To(Equal(members), "exports must preserve the original Hosts representation")

			},
				Entry("existing ID list", "host-1", "esxi2.example.com", "iSCSI", "NETAPP"),
				Entry("existing hostname list", " ESXI1.EXAMPLE.COM ", "esxi2.example.com", "iSCSI", "NETAPP"),
				Entry("ID/name collision", "host-1", "host-1", "", ""),
				Entry("valid member beside collision", "host-1, ESXI1.EXAMPLE.COM", "host-1", "iSCSI", "NETAPP"),
				Entry("duplicate names", "esxi1.example.com", " ESXI1.EXAMPLE.COM ", "", ""),
				Entry("shared datastore", "host-1,host-2", "esxi2.example.com", "FibreChannel, iSCSI", "NETAPP, PURE"),
			)
		})

		Context("datastore identity", func() {
			BeforeEach(func() {
				_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "Host", "Memory", "Template", "Powerstate") VALUES
('vm-a', 'vm-a', 'esxi-a.example.com', 1024, false, 'poweredOn'), ('vm-b', 'vm-b', 'host-b', 1024, false, 'poweredOn'), ('vm-legacy', 'vm-legacy', NULL, 1024, false, 'poweredOn');
INSERT INTO vhost ("Object ID", "Host", "Cluster") VALUES
('host-a', 'esxi-a.example.com', 'cluster-a'), ('host-b', 'esxi-b.example.com', 'cluster-b'), ('host-c', 'esxi-c.example.com', 'cluster-c');
INSERT INTO vhba ("Host ID", "Device", "Protocol") VALUES ('host-a', 'vmhba1', 'iSCSI'), ('host-b', 'vmhba1', 'FibreChannel');
INSERT INTO vhostscsi ("Host ID", "HBA Device", "Canonical Name", "LUN Vendor", "LUN Model") VALUES
('host-a', 'vmhba1', 'naa.first', 'NETAPP', 'FIRST'), ('host-a', 'vmhba1', 'naa.second', 'PURE', 'SECOND'), ('host-b', 'vmhba1', 'naa.shared', 'IBM', 'SHARED');
INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Backing Devices", "Type", "Storage Protocol") VALUES
('ds-a', 'shared', ' ESXI-A.EXAMPLE.COM ', '[]', 'NFS41', 'NFS'),
('ds-b', 'shared', 'host-b', '["naa.shared"]', 'VMFS', NULL),
('ds-unused', 'shared', 'host-c', '[]', 'VSAN', NULL),
('ds-repeat', 'first', 'host-a', '["naa.first"]', 'VMFS', NULL),
('ds-repeat', 'second', 'host-a', '["naa.second"]', 'VMFS', NULL),
('ds-ambiguous-1', 'ambiguous', 'host-a', '[]', 'VMFS', NULL),
('ds-ambiguous-2', 'ambiguous', 'esxi-a.example.com', '[]', 'VMFS', NULL),
('ds-legacy', 'legacy', NULL, '[]', 'VMFS', NULL);
INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Disk Path", "Capacity MiB", "Raw", "RDM LUN UUID") VALUES
('vm-a', '2000', '[shared] vm/same.vmdk', NULL, 100, false, NULL),
('vm-a', '2000', '[shared] vm/same.vmdk', NULL, 100, false, NULL),
('vm-a', '2002', '[first] vm/first.vmdk', NULL, 200, false, NULL),
('vm-a', '2003', '[second] vm/second.vmdk', NULL, 300, false, NULL),
('vm-a', '2004', '[ambiguous] vm/ambiguous.vmdk', NULL, 400, true, 'rdm-ambiguous'),
('vm-a', '2005', '[missing] vm/missing.vmdk', NULL, 500, false, NULL),
('vm-a', '2006', NULL, NULL, 600, false, NULL),
('vm-b', '2000', '[shared] vm/other.vmdk', NULL, 150, false, NULL),
('vm-legacy', '2000', NULL, '[legacy] vm/legacy.vmdk', 170, false, NULL);`)
				Expect(err).NotTo(HaveOccurred())
			})

			It("preserves disks and datastore row metadata in VM details", func() {
				vm, err := st.VM().Get(ctx, "vm-a")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(7))
				Expect(vm.DiskSize).To(Equal(int64(2200)))
				detail := v2.NewVirtualMachineDetailFromModel(*vm)
				Expect(detail.Disks).To(HaveLen(7))
				files := make(map[string]int)
				for i, disk := range vm.Disks {
					files[disk.File]++
					apiDisk := detail.Disks[i]
					Expect(*apiDisk.Capacity).To(Equal(disk.Capacity * 1024 * 1024))
					switch disk.File {
					case "[shared] vm/same.vmdk":
						Expect(disk.Datastore).NotTo(BeNil())
						Expect(disk.Datastore.ID).To(Equal("ds-a"))
						Expect(disk.Datastore.Type).To(Equal("NFS41"))
						Expect(disk.Datastore.ProtocolType).To(Equal("NFS"))
						Expect(disk.Datastore.StorageProtocol).To(Equal("File"))
						Expect(apiDisk.Datastore).NotTo(BeNil())
						Expect(*apiDisk.Datastore.Id).To(Equal("ds-a"))
						Expect(*apiDisk.Datastore.StorageProtocol).To(Equal("File"))
					case "[first] vm/first.vmdk", "[second] vm/second.vmdk":
						Expect(disk.Datastore).NotTo(BeNil())
						Expect(disk.Datastore.ID).To(Equal("ds-repeat"))
						vendor, model, backing := "NETAPP", "FIRST", "naa.first"
						if disk.File == "[second] vm/second.vmdk" {
							vendor, model, backing = "PURE", "SECOND", "naa.second"
						}
						Expect(disk.Datastore.Vendor).To(Equal(vendor))
						Expect(disk.Datastore.Model).To(Equal(model))
						Expect(disk.Datastore.BackingDeviceIDs).To(Equal([]string{backing}))
						Expect(disk.Datastore.ProtocolType).To(Equal("iSCSI"))
						Expect(*apiDisk.Datastore.Vendor).To(Equal(vendor))
					case "[ambiguous] vm/ambiguous.vmdk":
						Expect(disk.Datastore).To(BeNil())
						Expect(apiDisk.Datastore).To(BeNil())
						Expect(disk.RDM).To(BeTrue())
						Expect(disk.RDMBacking).NotTo(BeNil())
						Expect(disk.RDMBacking.LunUUID).To(Equal("rdm-ambiguous"))
						Expect(*apiDisk.RdmBacking.LunUuid).To(Equal("rdm-ambiguous"))
					case "[missing] vm/missing.vmdk", "":
						Expect(disk.Datastore).To(BeNil())
						Expect(apiDisk.Datastore).To(BeNil())
					default:
						Fail("unexpected disk: " + disk.File)
					}
				}
				Expect(files["[shared] vm/same.vmdk"]).To(Equal(2))

				vm, err = st.VM().Get(ctx, "vm-b")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(1))
				Expect(vm.Disks[0].Datastore).NotTo(BeNil())
				Expect(vm.Disks[0].Datastore.ID).To(Equal("ds-b"))
				Expect(vm.Disks[0].Datastore.Type).To(Equal("VMFS"))
				Expect(vm.Disks[0].Datastore.ProtocolType).To(Equal("FibreChannel"))
				Expect(vm.Disks[0].Datastore.Vendor).To(Equal("IBM"))

				vm, err = st.VM().Get(ctx, "vm-legacy")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(1))
				Expect(vm.Disks[0].Datastore).NotTo(BeNil())
				Expect(vm.Disks[0].Datastore.ID).To(Equal("ds-legacy"))
				Expect(vm.Disks[0].File).To(Equal("[legacy] vm/legacy.vmdk"))
			})

			DescribeTable("filters use the chosen datastore row", func(expression string, vmIDs []string) {
				filter := store.ByFilter(expression)
				Expect(filter).NotTo(BeNil())
				vms, err := st.VM().List(ctx, filter)
				Expect(err).NotTo(HaveOccurred())
				ids := make([]string, 0, len(vms))
				for _, vm := range vms {
					ids = append(ids, vm.ID)
				}
				Expect(ids).To(ConsistOf(vmIDs))
				count, err := st.VM().Count(ctx, filter)
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(len(vmIDs)))
			},
				Entry("datastore A", "datastore.object_id = 'ds-a'", []string{"vm-a"}),
				Entry("datastore B", "datastore.object_id = 'ds-b'", []string{"vm-b"}),
				Entry("repeated datastore ID", "datastore.object_id = 'ds-repeat'", []string{"vm-a"}),
				Entry("NFS datastore type", "datastore.type = 'NFS41'", []string{"vm-a"}),
				Entry("unused datastore type", "datastore.type = 'VSAN'", []string(nil)),
				Entry("unused datastore ID", "datastore.object_id = 'ds-unused'", []string(nil)),
				Entry("ambiguous datastore name", "datastore.name = 'ambiguous'", []string(nil)),
				Entry("ambiguous datastore ID", "datastore.object_id = 'ds-ambiguous-1'", []string(nil)),
				Entry("legacy datastore name", "datastore.name = 'legacy'", []string{"vm-legacy"}),
			)

			It("exports resolved VM counts without rewriting raw identities", func() {
				path := filepath.Join(GinkgoT().TempDir(), "datastores.csv")
				Expect(st.Export().CopyScope(ctx, "datastores", path)).NotTo(HaveOccurred())
				file, err := os.Open(path)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(file.Close)
				records, err := csv.NewReader(file).ReadAll()
				Expect(err).NotTo(HaveOccurred())
				Expect(records).To(HaveLen(9))
				name, id, hosts, count := slices.Index(records[0], "name"), slices.Index(records[0], "id"), slices.Index(records[0], "hosts"), slices.Index(records[0], "vm_count")
				Expect([]int{name, id, hosts, count}).NotTo(ContainElement(-1))
				for _, record := range records[1:] {
					switch record[id] {
					case "ds-a":
						Expect(record[name]).To(Equal("shared"))
						Expect(record[hosts]).To(Equal(" ESXI-A.EXAMPLE.COM "))
						Expect(record[count]).To(Equal("1"))
					case "ds-b":
						Expect(record[hosts]).To(Equal("host-b"))
						Expect(record[count]).To(Equal("1"))
					case "ds-repeat", "ds-legacy":
						Expect(record[count]).To(Equal("1"))
					case "ds-unused", "ds-ambiguous-1", "ds-ambiguous-2":
						Expect(record[count]).To(Equal("0"))
					default:
						Fail("unexpected datastore: " + record[id])
					}
				}
			})
		})
	})

	Context("legacy collections", func() {
		var db *sql.DB

		BeforeEach(func() {
			var err error
			db, err = sql.Open("duckdb", path)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(db.Close)
		})

		openReadOnlyCollection := func() {
			Expect(db.Close()).To(Succeed())
			database, err := pool.NewDatabase("legacy", path, time.Now(), store.EagerConnectionInitilization, 0, store.ReadOnlyDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(database)
			st, err = database.Store()
			Expect(err).NotTo(HaveOccurred())
		}

		Context("missing RDM LUN UUID column", func() {
			BeforeEach(func() {
				Expect(duckdb_parser.New(db, nil).Init()).NotTo(HaveOccurred())
				Expect(migrations.RunCollection(ctx, db, "collection")).NotTo(HaveOccurred())
				_, err := db.Exec(`ALTER TABLE vdisk DROP COLUMN "RDM LUN UUID";
 INSERT INTO vinfo ("VM ID","VM","Template") VALUES ('vm-1','legacy-vm',false);
 INSERT INTO vdisk ("VM ID","Disk Key","Raw","Capacity MiB","Disk UUID") VALUES ('vm-1','2000',true,100,'not-a-lun-uuid');`)
				Expect(err).NotTo(HaveOccurred())
			})
			It("reads legacy RDM collections without adding missing columns", func() {
				openReadOnlyCollection()
				vm, err := st.VM().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Disks).To(HaveLen(1))
				Expect(vm.Disks[0].RDM).To(BeTrue())
				data, err := json.Marshal(v2.NewVirtualMachineDetailFromModel(*vm))
				Expect(err).NotTo(HaveOccurred())
				var got struct {
					Disks []map[string]any `json:"disks"`
				}
				Expect(json.Unmarshal(data, &got)).NotTo(HaveOccurred())
				Expect(got.Disks[0]).NotTo(HaveKey("rdmBacking"))
				Expect(got.Disks[0]["capacity"]).To(Equal(float64(100 * 1024 * 1024)))
				var columns int
				Expect(st.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name='vdisk' AND column_name='RDM LUN UUID'`).Scan(&columns)).NotTo(HaveOccurred())
				Expect(columns).To(BeZero())
			})
		})

		Context("pre-storage schema", func() {
			BeforeEach(func() {
				// Use the pre-storage schema and original Agent migration, without
				// running the current Planner initializer or newer migrations.
				for _, file := range []string{"testdata/pre_storage_schema.sql", "migrations/collection/001_initial.sql"} {
					schema, err := os.ReadFile(file)
					Expect(err).NotTo(HaveOccurred())
					_, err = db.Exec(string(schema))
					Expect(err).NotTo(HaveOccurred())
				}
				_, err := db.Exec(`CREATE TABLE collection_schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT now());
INSERT INTO collection_schema_migrations (version) VALUES (1);
INSERT INTO vinfo ("VM ID", "VM", "Template") VALUES ('vm-1', 'legacy-vm', false);
INSERT INTO vhost ("Object ID", "Host", "Cluster") VALUES ('host-1', 'esxi.example.com', 'cluster1');
INSERT INTO vhba ("Device", "Type") VALUES ('vmhba64', 'ISCSI');
INSERT INTO vdatastore ("Object ID", "Name", "Hosts", "Type", "Backing Devices") VALUES ('ds-1', 'legacy-ds', 'esxi.example.com', 'VMFS', '["naa.extent"]');
INSERT INTO vdisk ("VM ID", "Disk Key", "Path", "Raw", "Capacity MiB", "Disk UUID") VALUES
('vm-1', '2000', '[legacy-ds] vm/raw.vmdk', true, 100, 'not-a-lun-uuid'),
('vm-1', '2001', '[legacy-ds] vm/ordinary.vmdk', false, 200, 'ordinary-serial');`)
				Expect(err).NotTo(HaveOccurred())
			})
			DescribeTable("pre-storage collection compatibility", func(members string, migrate bool) {
				_, err := db.Exec(`UPDATE vdatastore SET "Hosts"=?`, members)
				Expect(err).NotTo(HaveOccurred())
				if migrate {
					Expect(migrations.RunCollection(ctx, db, "collection")).NotTo(HaveOccurred())
					Expect(migrations.RunCollection(ctx, db, "collection")).NotTo(HaveOccurred())
				}
				openReadOnlyCollection()
				vm, err := st.VM().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(vm.Name).To(Equal("legacy-vm"))
				data, err := json.Marshal(v2.NewVirtualMachineDetailFromModel(*vm))
				Expect(err).NotTo(HaveOccurred())
				var got struct {
					Disks []map[string]any `json:"disks"`
				}
				Expect(json.Unmarshal(data, &got)).NotTo(HaveOccurred())
				Expect(got.Disks).To(HaveLen(2))
				for _, disk := range got.Disks {
					Expect(disk).NotTo(HaveKey("rdmBacking"))
					ds := disk["datastore"].(map[string]any)
					Expect(ds["name"]).To(Equal("legacy-ds"))
					Expect(ds["backingDeviceIds"]).To(Equal([]any{"naa.extent"}))
					for _, field := range []string{"filesystemType", "filesystemMajorVersion", "protocolType", "vendor", "model"} {
						Expect(ds).NotTo(HaveKey(field))
					}
					if disk["file"] == "[legacy-ds] vm/raw.vmdk" {
						Expect(disk["rdm"]).To(Equal(true))
						Expect(disk["capacity"]).To(Equal(float64(100 * 1024 * 1024)))
					} else {
						Expect(disk["file"]).To(Equal("[legacy-ds] vm/ordinary.vmdk"))
						Expect(disk["rdm"]).To(Equal(false))
						Expect(disk["capacity"]).To(Equal(float64(200 * 1024 * 1024)))
					}
				}
				if !migrate {
					var columns int
					Expect(st.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
WHERE table_name IN ('vdatastore', 'vdisk', 'vhba') AND column_name IN
('Filesystem Type', 'Filesystem Major Version', 'Storage Protocol', 'RDM LUN UUID', 'Host ID', 'Protocol')`).Scan(&columns)).NotTo(HaveOccurred())
					Expect(columns).To(BeZero(), "reading a historical collection must not upgrade it")
				}

			},
				Entry("read-only legacy names", "esxi.example.com", false),
				Entry("read-only legacy IDs", "host-1", false),
				Entry("upgraded names", "esxi.example.com", true),
				Entry("upgraded IDs", "host-1", true),
			)
		})
	})
})
