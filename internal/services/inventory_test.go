package services_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	api "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/converters"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("InventoryService", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		tmpDir string
		st     *store.Store
		srv    *services.InventoryService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "inventory-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		database, err := pool.NewDatabase("test", filepath.Join(tmpDir, "test.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		srv = services.NewInventoryService(st)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	DescribeTable("preserves host storage through cached and group inventory",
		func(lunUUID string) {
			// These are the normalized records produced by live and RVTools ingestion.
			_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "VI SDK UUID", "Host", "Cluster", "Template", "Powerstate") VALUES
('vm-1', 'vm1', 'vc-1', 'esxi1', 'cluster1', false, 'poweredOn'),
('vm-2', 'vm2', 'vc-1', 'esxi2', 'cluster2', false, 'poweredOn');
INSERT INTO vcluster ("Object ID", "Name") VALUES ('domain-c1', 'cluster1'), ('domain-c2', 'cluster2');
INSERT INTO vhost ("Object ID", "Host", "Cluster") VALUES ('host-1', 'esxi1', 'cluster1'), ('host-2', 'esxi2', 'cluster2');
INSERT INTO vhba ("Host ID", "Device", "HBA Key", "Protocol", "Node WWN", "Port WWN") VALUES
('host-1', 'vmhba1', 'hba-1', 'FibreChannel', '000123', '000456'),
('host-2', 'vmhba1', 'hba-1', 'iSCSI', NULL, NULL);
INSERT INTO vhostscsi ("Host ID", "Canonical Name", "SCSI Disk Key", "HBA Device", "LUN UUID") VALUES
('host-1', 'naa.shared', 'lun-1', 'vmhba1', ?),
('host-2', 'naa.shared', 'lun-1', 'vmhba9', NULL);`, lunUUID)
			Expect(err).NotTo(HaveOccurred())
			parser := duckdb_parser.New(st.Querier(), nil)
			inv, err := parser.BuildInventory(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			raw, err := json.Marshal(converters.ToAPI(inv))
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Inventory().Save(ctx, raw)).To(Succeed())
			cached, err := srv.GetInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			var response api.Inventory
			Expect(json.Unmarshal(cached.Data, &response)).To(Succeed())

			assertHosts := func(hosts []api.Host, expectedIDs ...string) {
				Expect(hosts).To(HaveLen(len(expectedIDs)))
				ids := make([]string, 0, len(hosts))
				for _, host := range hosts {
					Expect(host.Id).NotTo(BeNil())
					ids = append(ids, *host.Id)
					data, err := json.Marshal(host)
					Expect(err).NotTo(HaveOccurred())
					var fields map[string]json.RawMessage
					Expect(json.Unmarshal(data, &fields)).To(Succeed())
					Expect(fields).To(HaveKey("storageAdapters"))
					Expect(fields).To(HaveKey("storageLuns"))
					if *host.Id == "host-1" {
						Expect(string(fields["storageAdapters"])).To(MatchJSON(`[{"device":"vmhba1","key":"hba-1","protocol":"FibreChannel","nodeWwn":"000123","portWwn":"000456"}]`))
						wantLun := `[{"canonicalName":"naa.shared","key":"lun-1","adapterDevices":["vmhba1"]}]`
						if lunUUID != "" {
							wantLun = `[{"canonicalName":"naa.shared","key":"lun-1","lunUuid":"000lun-uuid","adapterDevices":["vmhba1"]}]`
						}
						Expect(string(fields["storageLuns"])).To(MatchJSON(wantLun))
					} else {
						Expect(string(fields["storageAdapters"])).To(MatchJSON(`[{"device":"vmhba1","key":"hba-1","protocol":"iSCSI"}]`))
						Expect(string(fields["storageLuns"])).To(MatchJSON(`[{"canonicalName":"naa.shared","key":"lun-1","adapterDevices":["vmhba9"]}]`))
					}
				}
				Expect(ids).To(ConsistOf(expectedIDs))
			}
			Expect(response.Vcenter).NotTo(BeNil())
			Expect(response.Vcenter.Infra.Hosts).NotTo(BeNil())
			assertHosts(*response.Vcenter.Infra.Hosts, "host-1", "host-2")
			for name, cluster := range response.Clusters {
				Expect(cluster.Infra.Hosts).NotTo(BeNil())
				if name == "domain-c1" {
					assertHosts(*cluster.Infra.Hosts, "host-1")
				} else {
					assertHosts(*cluster.Infra.Hosts, "host-2")
				}
			}

			groups := services.NewGroupService(st, parser)
			created, err := groups.Create(ctx, models.Group{Name: "first-cluster", Filter: "cluster = 'cluster1'"})
			Expect(err).NotTo(HaveOccurred())
			stored, err := groups.Get(ctx, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Inventory).NotTo(BeNil())
			groupInventory := converters.ToAPI(stored.Inventory)
			// Group host membership is unchanged: the vCenter view still includes all hosts.
			assertHosts(*groupInventory.Vcenter.Infra.Hosts, "host-1", "host-2")
			for name, cluster := range groupInventory.Clusters {
				if name == "domain-c1" {
					assertHosts(*cluster.Infra.Hosts, "host-1")
				} else {
					assertHosts(*cluster.Infra.Hosts, "host-2")
				}
			}
		},
		Entry("live facts without a host LUN UUID", ""),
		Entry("RVTools facts with a host LUN UUID", "000lun-uuid"),
	)

	Context("GetInventory", func() {
		// Given no inventory has been collected
		// When we request the inventory
		// Then it should return a not-found error
		It("should return not found when no inventory exists", func() {
			// Act
			inv, err := srv.GetInventory(ctx)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(inv).To(BeNil())
		})

		// Given inventory data has been inserted via raw SQL
		// When we request the inventory through the service
		// Then it should return the stored inventory data
		It("should return inventory after raw SQL insert", func() {
			// Arrange
			inventoryJSON := `{"vcenter_id":"vc-123","clusters":{},"vcenter":{"infra":{"hosts":[{"id":"host-1","vendor":"vendor","model":"model"}]}}}`
			_, err := st.Querier().ExecContext(ctx,
				`INSERT INTO inventory (id, data) VALUES (1, ?)`, []byte(inventoryJSON))
			Expect(err).NotTo(HaveOccurred())

			// Act
			inv, err := srv.GetInventory(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).NotTo(BeNil())
			Expect(string(inv.Data)).To(Equal(inventoryJSON))
			Expect(inv.CreatedAt).NotTo(BeZero())
			Expect(inv.UpdatedAt).NotTo(BeZero())
		})

		// Given inventory data was saved through the store
		// When we request the inventory through the service
		// Then it should return the same data
		It("should return inventory saved through store", func() {
			// Arrange
			data := []byte(`{"vcenter_id":"vc-456"}`)
			Expect(st.Inventory().Save(ctx, data)).To(Succeed())

			// Act
			inv, err := srv.GetInventory(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(inv).NotTo(BeNil())
			Expect(string(inv.Data)).To(Equal(`{"vcenter_id":"vc-456"}`))
		})
	})
})
