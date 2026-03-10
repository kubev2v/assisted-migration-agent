package services_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("InventoryService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		srv *services.InventoryService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx)).To(Succeed())

		srv = services.NewInventoryService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
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
			_, err := db.ExecContext(ctx,
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
