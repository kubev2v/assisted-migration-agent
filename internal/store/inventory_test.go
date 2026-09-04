package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("InventoryStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "inventory-store-test-*")
		Expect(err).NotTo(HaveOccurred())
		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		s, err = collDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
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
