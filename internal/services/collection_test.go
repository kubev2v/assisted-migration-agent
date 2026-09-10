package services_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("CollectionService", func() {
	var (
		ctx            context.Context
		pool           *store.Pool
		tmpDir         string
		collectionsDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "collection-svc-test-*")
		Expect(err).NotTo(HaveOccurred())

		collectionsDir = filepath.Join(tmpDir, "collections")
		Expect(os.Mkdir(collectionsDir, 0o755)).To(Succeed())

		pool = store.NewPool(5 * time.Minute)

		mainPath := filepath.Join(tmpDir, "agent.duckdb")
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
	})

	AfterEach(func() {
		pool.Close()
		// Restore permissions in case a test made a subdirectory read-only.
		Expect(os.Chmod(collectionsDir, 0o755)).To(Succeed())
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	addCollectionDB := func(name string) {
		dbPath := filepath.Join(collectionsDir, name+".duckdb")
		f, err := os.Create(dbPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())

		db, err := pool.NewDatabase(name, dbPath, time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
	}

	Describe("DeleteAll", func() {
		It("should delete all collection databases and their files", func() {
			addCollectionDB("collection_1")
			addCollectionDB("collection_2")

			svc := services.NewCollectionService(pool)
			Expect(svc.List()).To(HaveLen(2))

			err := svc.DeleteAll(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.List()).To(BeEmpty())

			_, err = os.Stat(filepath.Join(collectionsDir, "collection_1.duckdb"))
			Expect(os.IsNotExist(err)).To(BeTrue())
			_, err = os.Stat(filepath.Join(collectionsDir, "collection_2.duckdb"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should mark collections as pending_delete when disk removal fails", func() {
			addCollectionDB("collection_1")

			// Make the collection file undeletable by removing write permission
			// on its directory. The main DB is in the parent dir so it stays writable.
			Expect(os.Chmod(collectionsDir, 0o555)).To(Succeed())

			svc := services.NewCollectionService(pool)
			err := svc.DeleteAll(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.List()).To(BeEmpty())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			mainStore, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := mainStore.Collection().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(HaveLen(1))
			Expect(collections[0].Database).To(Equal("collection_1"))
			Expect(collections[0].State).To(Equal(models.CollectionStatePendingDelete))
		})
	})
})
