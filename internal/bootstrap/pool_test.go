package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestPool(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bootstrap Pool Suite")
}

var _ = Describe("Pool", func() {
	Describe("CleanupStaleCollections", func() {
		var (
			tmpDir   string
			seedPool *store.Pool
			st       *store.Store
		)

		BeforeEach(func() {
			var err error
			tmpDir, err = os.MkdirTemp("", "initpool-test-*")
			Expect(err).NotTo(HaveOccurred())

			seedPool = store.NewPool(5 * time.Minute)
			db, err := seedPool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			seedPool.Add(db)
			Expect(db.Migrate(context.Background(), migrations.RunMain)).To(Succeed())

			st, err = db.Store()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			seedPool.Close()
			if tmpDir != "" {
				_ = os.RemoveAll(tmpDir)
			}
		})

		It("returns all file deletion failures while excluding stale collections and cleaning up other files", func() {
			for _, name := range []string{"collection_1000", "collection_2000", "collection_3000"} {
				_, err := st.Collection().Create(context.Background(), name)
				Expect(err).NotTo(HaveOccurred())
				path := filepath.Join(tmpDir, name+".duckdb")
				if name == "collection_3000" {
					Expect(os.WriteFile(path, []byte("stale"), 0600)).To(Succeed())
					continue
				}
				Expect(os.Mkdir(path, 0700)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(path, "block-deletion"), nil, 0600)).To(Succeed())
			}
			seedPool.Close()
			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()

			err = bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)
			Expect(err).To(MatchError(And(
				ContainSubstring("failed to remove stale collection file"),
				ContainSubstring("collection_1000.duckdb"),
				ContainSubstring("collection_2000.duckdb"),
			)))
			Expect(pool.List()).To(HaveLen(1))
			_, err = pool.Latest()
			Expect(err).To(HaveOccurred())
			Expect(filepath.Join(tmpDir, "collection_3000.duckdb")).NotTo(BeAnExistingFile())
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			mainStore, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())
			collections, err := mainStore.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(HaveLen(2))
			for _, name := range []string{"collection_1000", "collection_2000"} {
				Expect(os.Remove(filepath.Join(tmpDir, name+".duckdb", "block-deletion"))).To(Succeed())
			}

			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())
			collections, err = mainStore.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())
		})

		It("returns marker deletion failures without exposing the stale collection and allows retry", func() {
			_, err := st.Collection().Create(context.Background(), "collection_1000")
			Expect(err).NotTo(HaveOccurred())
			file := filepath.Join(tmpDir, "collection_1000.duckdb")
			Expect(os.WriteFile(file, []byte("stale"), 0600)).To(Succeed())
			seedPool.Close()
			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Unregister(store.MainDatabaseID)).To(Succeed())
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.LazyConnectionInitilization, 256, store.ReadOnlyDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(mainDB)

			err = bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)
			Expect(err).To(MatchError(ContainSubstring("failed to delete stale collection marker collection_1000")))
			Expect(pool.List()).To(HaveLen(1))
			_, err = pool.Latest()
			Expect(err).To(HaveOccurred())
			Expect(file).NotTo(BeAnExistingFile())
			mainStore, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())
			collections, err := mainStore.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(HaveLen(1))
			pool.Close()

			pool, err = bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())
			mainDB, err = pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			mainStore, err = mainDB.Store()
			Expect(err).NotTo(HaveOccurred())
			collections, err = mainStore.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())
		})

		It("should remove failed collection markers and their files on startup", func() {
			_, err := st.Collection().Create(context.Background(), "collection_1000")
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Collection().MarkFailed(context.Background(), "collection_1000", "vcenter error")).To(Succeed())

			dbFile := filepath.Join(tmpDir, "collection_1000.duckdb")
			Expect(os.WriteFile(dbFile, []byte("dummy"), 0644)).To(Succeed())

			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Migrate(context.Background())).To(Succeed())
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())

			_, err = os.Stat(dbFile)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should remove stale running collection markers and their files on startup", func() {
			_, err := st.Collection().Create(context.Background(), "collection_2000")
			Expect(err).NotTo(HaveOccurred())

			dbFile := filepath.Join(tmpDir, "collection_2000.duckdb")
			Expect(os.WriteFile(dbFile, []byte("dummy"), 0644)).To(Succeed())

			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Migrate(context.Background())).To(Succeed())
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())

			_, err = os.Stat(dbFile)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should succeed when the duckdb file is already missing", func() {
			_, err := st.Collection().Create(context.Background(), "collection_3000")
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Collection().MarkFailed(context.Background(), "collection_3000", "crash")).To(Succeed())

			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Migrate(context.Background())).To(Succeed())
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())
		})

		It("should be a no-op when no stale collections exist", func() {
			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Migrate(context.Background())).To(Succeed())
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())
		})

		It("should clean up multiple stale collections on startup", func() {
			_, err := st.Collection().Create(context.Background(), "collection_4000")
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Collection().MarkFailed(context.Background(), "collection_4000", "err1")).To(Succeed())

			_, err = st.Collection().Create(context.Background(), "collection_5000")
			Expect(err).NotTo(HaveOccurred())

			file1 := filepath.Join(tmpDir, "collection_4000.duckdb")
			Expect(os.WriteFile(file1, []byte("dummy"), 0644)).To(Succeed())
			file2 := filepath.Join(tmpDir, "collection_5000.duckdb")
			Expect(os.WriteFile(file2, []byte("dummy"), 0644)).To(Succeed())

			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.Migrate(context.Background())).To(Succeed())
			Expect(bootstrap.CleanupStaleCollections(context.Background(), pool, tmpDir)).To(Succeed())

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())

			_, err = os.Stat(file1)
			Expect(os.IsNotExist(err)).To(BeTrue())
			_, err = os.Stat(file2)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})
