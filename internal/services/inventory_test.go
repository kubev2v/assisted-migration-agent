package services_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"uuid"

	api "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
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
			inventoryJSON := `{"vcenter_id":"vc-123","clusters":{},"vcenter":{}}`
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

	Context("DeleteVCenterInventory", func() {
		vcenterOne := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		vcenterTwo := uuid.MustParse("22222222-2222-2222-2222-222222222222")

		BeforeEach(func() {
			Expect(st.Inventory().SaveSubset(ctx, []byte(fmt.Sprintf(`{"vcenter_id":%q}`, vcenterOne)))).To(Succeed())
			Expect(st.Inventory().SaveSubset(ctx, []byte(fmt.Sprintf(`{"vcenter_id":%q}`, vcenterTwo)))).To(Succeed())

			_, err := st.Querier().ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO vinfo ("VM ID", "VM", "VI SDK UUID") VALUES
					('vm-one', 'first', '%s'),
					('vm-two', 'second', '%s')
			`, vcenterOne, vcenterTwo))
			Expect(err).NotTo(HaveOccurred())
		})

		It("deletes only the specified vCenter inventory and its VMs", func() {
			Expect(srv.DeleteVCenterInventory(ctx, vcenterOne)).To(Succeed())

			_, err := st.Inventory().GetByID(ctx, vcenterOne.String())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			_, err = st.Inventory().GetByID(ctx, vcenterTwo.String())
			Expect(err).NotTo(HaveOccurred())

			var vmID string
			err = st.Querier().QueryRowContext(ctx, `SELECT "VM ID" FROM vinfo`).Scan(&vmID)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmID).To(Equal("vm-two"))
		})

		It("rebuilds the main inventory from the remaining VMs", func() {
			Expect(srv.DeleteVCenterInventory(ctx, vcenterOne)).To(Succeed())

			stored, err := srv.GetAggregatedInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.VcenterIDs).To(ConsistOf(vcenterTwo.String()))

			var rebuilt api.Inventory
			Expect(json.Unmarshal(stored.Data, &rebuilt)).To(Succeed())
			Expect(rebuilt.VcenterId).To(Equal("aggregated"))
			Expect(rebuilt.VcenterVersion).NotTo(BeNil())
			Expect(*rebuilt.VcenterVersion).To(Equal("aggregated"))
			Expect(rebuilt.Vcenter).NotTo(BeNil())
			Expect(rebuilt.Vcenter.Vms.Total).To(Equal(1))
		})

		It("refreshes group matches and inventories", func() {
			groupSvc := services.NewGroupService(st, duckdb_parser.New(st.Querier(), nil))

			removedGroup, err := groupSvc.Create(ctx, models.Group{
				Name:   "removed-vms",
				Filter: "name = 'first'",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(removedGroup.Inventory).NotTo(BeNil())

			retainedGroup, err := groupSvc.Create(ctx, models.Group{
				Name:   "retained-vms",
				Filter: "name = 'second'",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(retainedGroup.Inventory).NotTo(BeNil())

			Expect(srv.DeleteVCenterInventory(ctx, vcenterOne)).To(Succeed())

			removedGroup, err = groupSvc.Get(ctx, removedGroup.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(removedGroup.Inventory).To(BeNil())

			retainedGroup, err = groupSvc.Get(ctx, retainedGroup.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(retainedGroup.Inventory).NotTo(BeNil())
			Expect(retainedGroup.Inventory.VCenter.VMs.Total).To(Equal(1))

			groups, _, err := groupSvc.List(ctx, services.GroupListParams{})
			Expect(err).NotTo(HaveOccurred())
			counts := make(map[string]int)
			for _, group := range groups {
				counts[group.ID.String()] = group.VmCount
			}
			Expect(counts[removedGroup.ID.String()]).To(BeZero())
			Expect(counts[retainedGroup.ID.String()]).To(Equal(1))
		})

		It("returns not found without deleting VMs for a missing inventory", func() {
			original := []byte(`{"vcenter_id":"aggregated","vcenter":{"vms":{"total":2}}}`)
			Expect(st.Inventory().Save(ctx, original)).To(Succeed())

			groupSvc := services.NewGroupService(st, duckdb_parser.New(st.Querier(), nil))
			group, err := groupSvc.Create(ctx, models.Group{
				Name:   "retained-vms",
				Filter: "name = 'second'",
			})
			Expect(err).NotTo(HaveOccurred())

			err = srv.DeleteVCenterInventory(ctx, uuid.MustParse("33333333-3333-3333-3333-333333333333"))
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())

			var count int
			err = st.Querier().QueryRowContext(ctx, `SELECT count(*) FROM vinfo`).Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))

			stored, err := srv.GetInventory(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Data).To(Equal(original))

			unchanged, err := groupSvc.Get(ctx, group.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(unchanged.Inventory).To(Equal(group.Inventory))
			groups, _, err := groupSvc.List(ctx, services.GroupListParams{})
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].VmCount).To(Equal(1))
		})

		It("deletes a VM with related disk data", func() {
			_, err := st.Querier().ExecContext(ctx, `
				INSERT INTO vdisk ("VM ID", "Capacity MiB") VALUES ('vm-one', 1024)
			`)
			Expect(err).NotTo(HaveOccurred())

			Expect(srv.DeleteVCenterInventory(ctx, vcenterOne)).To(Succeed())

			var count int
			err = st.Querier().QueryRowContext(ctx, `SELECT count(*) FROM vdisk WHERE "VM ID" = 'vm-one'`).Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(BeZero())
		})
	})
})
