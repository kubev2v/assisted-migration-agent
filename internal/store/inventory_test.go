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
		It("should save inventory successfully", func() {
			data := []byte(`{"vms": [{"name": "vm1"}]}`)
			err := s.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update inventory on second save (upsert)", func() {
			// First save
			data1 := []byte(`{"vms": [{"name": "vm1"}]}`)
			err := s.Inventory().Save(ctx, data1)
			Expect(err).NotTo(HaveOccurred())

			// Update inventory
			data2 := []byte(`{"vms": [{"name": "vm1"}, {"name": "vm2"}]}`)
			err = s.Inventory().Save(ctx, data2)
			Expect(err).NotTo(HaveOccurred())

			// Verify updated values
			retrieved, err := s.Inventory().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Data).To(Equal(data2))
		})
	})

	Describe("Get", func() {
		It("should return ResourceNotFoundError when no inventory exists", func() {
			_, err := s.Inventory().Get(ctx)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should retrieve saved inventory", func() {
			data := []byte(`{"vms": [{"name": "vm1"}]}`)
			err := s.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Inventory().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Data).To(Equal(data))
		})

		It("should have timestamps set by database", func() {
			data := []byte(`{"vms": []}`)
			err := s.Inventory().Save(ctx, data)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Inventory().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.CreatedAt).NotTo(BeZero())
			Expect(retrieved.UpdatedAt).NotTo(BeZero())
		})
	})
})
