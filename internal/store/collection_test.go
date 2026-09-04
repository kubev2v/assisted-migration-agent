package store_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("CollectionStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "collection-store-test-*")
		Expect(err).NotTo(HaveOccurred())
		pool = store.NewPool(5 * time.Minute)
		mainDB, dbErr := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		s, err = mainDB.Store()
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

	Context("Create", func() {
		It("should persist a collection with running state", func() {
			created, err := s.Collection().Create(ctx, "collection_1000")
			Expect(err).NotTo(HaveOccurred())
			Expect(created.Database).To(Equal("collection_1000"))
			Expect(created.State).To(Equal(models.CollectionStateRunning))
		})
	})

	Context("List", func() {
		It("should return empty when no collections exist", func() {
			results, err := s.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should return all collections", func() {
			_, err := s.Collection().Create(ctx, "collection_1000")
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Collection().Create(ctx, "collection_2000")
			Expect(err).NotTo(HaveOccurred())

			results, err := s.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
		})
	})

	Context("MarkFailed", func() {
		It("should transition state to failed and record error", func() {
			_, err := s.Collection().Create(ctx, "collection_1000")
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Collection().MarkFailed(ctx, "collection_1000", "something went wrong")).To(Succeed())

			results, err := s.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].State).To(Equal(models.CollectionStateFailed))
			Expect(results[0].Error).To(Equal("something went wrong"))
		})

		It("should return error when database not found or not running", func() {
			err := s.Collection().MarkFailed(ctx, "nonexistent", "err")
			Expect(err).To(HaveOccurred())
		})

		It("should reject marking an already-failed collection", func() {
			_, err := s.Collection().Create(ctx, "collection_1000")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Collection().MarkFailed(ctx, "collection_1000", "first")).To(Succeed())

			err = s.Collection().MarkFailed(ctx, "collection_1000", "second")
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Delete", func() {
		It("should remove the collection from the store", func() {
			_, err := s.Collection().Create(ctx, "collection_1000")
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Collection().Delete(ctx, "collection_1000")).To(Succeed())

			results, err := s.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})

		It("should succeed silently when database does not exist", func() {
			err := s.Collection().Delete(ctx, "nonexistent")
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
