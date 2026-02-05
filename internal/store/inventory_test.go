package store_test

import (
	"context"
	"database/sql"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InventoryStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Describe("Save", func() {
		// Given valid inventory data
		// When we save the inventory
		// Then it should save successfully without error
		It("should save inventory successfully", func() {
			// Arrange
			data := []byte(`{"vms": [{"name": "vm1"}]}`)

			// Act
			err := s.Inventory().Save(ctx, data)

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})

		// Given existing inventory in the store
		// When we save new inventory data
		// Then it should update the existing record (upsert)
		It("should update inventory on second save (upsert)", func() {
			// Arrange
			data1 := []byte(`{"vms": [{"name": "vm1"}]}`)
			err := s.Inventory().Save(ctx, data1)
			Expect(err).NotTo(HaveOccurred())

			// Act
			data2 := []byte(`{"vms": [{"name": "vm1"}, {"name": "vm2"}]}`)
			err = s.Inventory().Save(ctx, data2)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			retrieved, err := s.Inventory().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Data).To(Equal(data2))
		})
	})

	Describe("Get", func() {
		// Given an empty inventory store
		// When we try to get the inventory
		// Then it should return ResourceNotFoundError
		It("should return ResourceNotFoundError when no inventory exists", func() {
			// Act
			_, err := s.Inventory().Get(ctx)

			// Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given saved inventory in the store
		// When we retrieve the inventory
		// Then it should return the saved data
		It("should retrieve saved inventory", func() {
			// Arrange
			data := []byte(`{"vms": [{"name": "vm1"}]}`)
			err := s.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Inventory().Get(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Data).To(Equal(data))
		})

		// Given saved inventory in the store
		// When we retrieve the inventory
		// Then it should have timestamps set by the database
		It("should have timestamps set by database", func() {
			// Arrange
			data := []byte(`{"vms": []}`)
			err := s.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Inventory().Get(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.CreatedAt).NotTo(BeZero())
			Expect(retrieved.UpdatedAt).NotTo(BeZero())
		})
	})
})
