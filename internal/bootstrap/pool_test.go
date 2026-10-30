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
	Describe("InitPool stale collection cleanup", func() {
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
			Expect(db.Migrate(context.Background(), migrations.RunMain)).To(Succeed())

			st, err = db.Store()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if tmpDir != "" {
				_ = os.RemoveAll(tmpDir)
			}
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

			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			st, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())

			collections, err := st.Collection().List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(collections).To(BeEmpty())
		})

		It("should fail startup when a collection database cannot be migrated", func() {
			seedPool.Close()
			broken := filepath.Join(tmpDir, "collection_1234.duckdb")
			Expect(os.WriteFile(broken, []byte("not a duckdb file"), 0644)).To(Succeed())

			_, err := bootstrap.InitPool(tmpDir)

			Expect(err).To(MatchError(ContainSubstring("collection_1234")))
			Expect(os.ReadFile(broken)).To(Equal([]byte("not a duckdb file")))
		})

		It("should be a no-op when no stale collections exist", func() {
			seedPool.Close()

			pool, err := bootstrap.InitPool(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()

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
