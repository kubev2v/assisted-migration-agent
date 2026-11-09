package bootstrap_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
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

var _ = Describe("Startup migration policy", func() {
	var folder string

	BeforeEach(func() {
		folder = GinkgoT().TempDir()
	})

	DescribeTable("discovers collections in literal directory paths",
		func(name string) {
			folder = filepath.Join(folder, name)
			Expect(os.Mkdir(folder, 0700)).To(Succeed())
			collection := filepath.Join(folder, "collection_100.duckdb")
			Expect(os.WriteFile(collection, nil, 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(folder, "notes.txt"), nil, 0600)).To(Succeed())

			pool, err := bootstrap.InitPool(folder)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.List()).To(HaveLen(2))
			latest, err := pool.Latest()
			Expect(err).NotTo(HaveOccurred())
			Expect(latest.Path).To(Equal(collection))
		},
		Entry("character class", "data[1]"),
		Entry("unmatched bracket", "data["),
		Entry("wildcards", "data*?"),
	)

	It("returns collection discovery errors", func() {
		pool, err := bootstrap.InitPool(filepath.Join(folder, "missing"))
		Expect(pool).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("failed to list collection databases")))
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	})

	It("rejects colliding collection IDs without opening or modifying either database", func() {
		seen := make(map[[3]byte]string)
		var paths []string
		for timestamp := 1; len(paths) < 2; timestamp++ {
			path := filepath.Join(folder, fmt.Sprintf("collection_%d.duckdb", timestamp))
			hash := sha256.Sum256([]byte(path))
			key := [3]byte{hash[0], hash[1], hash[2]}
			previous, exists := seen[key]
			if exists {
				paths = []string{previous, path}
				break
			}
			seen[key] = path
		}
		contents := []byte("leave this database alone")
		for _, path := range paths {
			Expect(os.WriteFile(path, contents, 0600)).To(Succeed())
		}

		pool, err := bootstrap.InitPool(folder)
		Expect(pool).To(BeNil())
		Expect(err).To(MatchError(And(
			ContainSubstring("collection database ID collision"),
			ContainSubstring(paths[0]),
			ContainSubstring(paths[1]),
		)))
		Expect(filepath.Join(folder, "agent.duckdb")).NotTo(BeAnExistingFile())
		for _, path := range paths {
			actual, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(contents))
		}
	})

	It("only registers a missing main database until migration is requested", func() {
		pool, err := bootstrap.InitPool(folder)
		Expect(err).NotTo(HaveOccurred())
		defer pool.Close()
		Expect(pool.List()).To(HaveLen(1))
		Expect(filepath.Join(folder, "agent.duckdb")).NotTo(BeAnExistingFile())

		Expect(pool.Migrate(context.Background())).To(Succeed())
		Expect(filepath.Join(folder, "agent.duckdb")).To(BeAnExistingFile())
	})

	It("initializes a fresh main database when automatic migration is enabled", func() {
		pool, err := startPool(folder, true)
		Expect(err).NotTo(HaveOccurred())
		pool.Close()
		db := openStartupDatabase(folder, "agent.duckdb")
		Expect(migrations.ValidateMain(context.Background(), db)).To(Succeed())
	})

	It("does not create a missing main database when automatic migration is disabled", func() {
		pool, err := startPool(folder, false)
		Expect(pool).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("requires an existing migrated main database")))
		_, err = os.Stat(filepath.Join(folder, "agent.duckdb"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	Context("with existing databases", func() {
		BeforeEach(func() {
			db := openStartupDatabase(folder, "agent.duckdb")
			Expect(migrations.RunMain(context.Background(), db)).To(Succeed())
			_, err := db.Exec(`INSERT INTO configuration(id, agent_mode) VALUES (1, 'connected')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
		})

		DescribeTable("keeps malformed filenames out of automatic startup and rejects them during strict startup",
			func(filename string) {
				seedStartupCollection(folder)
				malformed := filepath.Join(folder, filename)
				contents := []byte("leave this file alone")
				Expect(os.WriteFile(malformed, contents, 0600)).To(Succeed())

				pool, err := startPool(folder, false)
				Expect(pool).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("failed to validate collection filename " + filename)))
				db := openStartupDatabase(folder, "collection_100.duckdb")
				var version int
				Expect(db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version)).To(Succeed())
				Expect(version).To(Equal(1))
				Expect(db.Close()).To(Succeed())

				pool, err = startPool(folder, true)
				Expect(err).NotTo(HaveOccurred())
				defer pool.Close()
				Expect(pool.List()).To(HaveLen(2))
				latest, err := pool.Latest()
				Expect(err).NotTo(HaveOccurred())
				Expect(latest.Path).To(Equal(filepath.Join(folder, "collection_100.duckdb")))
				db = openStartupDatabase(folder, "collection_100.duckdb")
				Expect(migrations.ValidateCollection(context.Background(), db, "collection_100")).To(Succeed())
				actual, err := os.ReadFile(malformed)
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(Equal(contents))
			},
			Entry("non-timestamp", "collection_backup.duckdb"),
			Entry("missing timestamp", "collection_.duckdb"),
			Entry("timestamp overflow", "collection_9223372036854775808.duckdb"),
		)

		It("discovers pending collections without migrating them or cleaning up markers", func() {
			seedStartupCollection(folder)
			db := openStartupDatabase(folder, "agent.duckdb")
			_, err := db.Exec(`INSERT INTO collections ("database",state) VALUES ('collection_100','failed')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())

			pool, err := bootstrap.InitPool(folder)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.List()).To(HaveLen(2))

			db = openStartupDatabase(folder, "collection_100.duckdb")
			var version int
			Expect(db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version)).To(Succeed())
			Expect(version).To(Equal(1))
			db = openStartupDatabase(folder, "agent.duckdb")
			var markers int
			Expect(db.QueryRow(`SELECT count(*) FROM collections`).Scan(&markers)).To(Succeed())
			Expect(markers).To(Equal(1))
		})

		It("validates pending collections in a literal directory before startup", func() {
			literal := filepath.Join(folder, "data[1]")
			Expect(os.Mkdir(literal, 0700)).To(Succeed())
			db := openStartupDatabase(literal, "agent.duckdb")
			Expect(migrations.RunMain(context.Background(), db)).To(Succeed())
			Expect(db.Close()).To(Succeed())
			seedStartupCollection(literal)

			pool, err := startPool(literal, false)
			Expect(pool).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("collection_100")))
			Expect(err).To(MatchError(ContainSubstring("apply migrations offline or enable --auto-migrate")))
		})

		It("starts with current schemas without rewriting migration history", func() {
			seedStartupCollection(folder)
			db := openStartupDatabase(folder, "collection_100.duckdb")
			Expect(migrations.RunCollection(context.Background(), db, "collection_100")).To(Succeed())
			_, err := db.Exec(`UPDATE collection_schema_migrations SET applied_at='2000-01-01'`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())

			mainHistory := readStartupMainHistory(folder)
			collectionHistory := readStartupCollectionHistory(folder)
			pool, err := startPool(folder, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(pool.List()).To(HaveLen(2))
			pool.Close()
			Expect(readStartupMainHistory(folder)).To(Equal(mainHistory))
			Expect(readStartupCollectionHistory(folder)).To(Equal(collectionHistory))
		})

		DescribeTable("rejects invalid main histories without repairing them",
			func(query string, expectedVersions int) {
				db := openStartupDatabase(folder, "agent.duckdb")
				_, err := db.Exec(query)
				Expect(err).NotTo(HaveOccurred())
				Expect(db.Close()).To(Succeed())
				pool, err := startPool(folder, false)
				Expect(pool).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("failed to validate main database")))
				db = openStartupDatabase(folder, "agent.duckdb")
				var count int
				Expect(db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count)).To(Succeed())
				Expect(count).To(Equal(expectedVersions))
				var mode string
				Expect(db.QueryRow(`SELECT agent_mode FROM configuration`).Scan(&mode)).To(Succeed())
				Expect(mode).To(Equal("connected"))
			},
			Entry("empty history", `DELETE FROM schema_migrations`, 0),
			Entry("newer history", `INSERT INTO schema_migrations(version) VALUES (2)`, 2),
			Entry("gapped history", `UPDATE schema_migrations SET version=2`, 1),
		)

		It("does not recreate a missing history table", func() {
			db := openStartupDatabase(folder, "agent.duckdb")
			_, err := db.Exec(`DROP TABLE schema_migrations`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			pool, err := startPool(folder, false)
			Expect(pool).To(BeNil())
			Expect(err).To(HaveOccurred())
			db = openStartupDatabase(folder, "agent.duckdb")
			var tables int
			Expect(db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name='schema_migrations'`).Scan(&tables)).To(Succeed())
			Expect(tables).To(BeZero())
		})

		It("fails on pending collection migrations before deleting stale data", func() {
			seedStartupCollection(folder)
			db := openStartupDatabase(folder, "agent.duckdb")
			_, err := db.Exec(`INSERT INTO collections ("database",state) VALUES ('stale','failed')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			stale := filepath.Join(folder, "stale.duckdb")
			Expect(os.WriteFile(stale, []byte("retain until validation succeeds"), 0600)).To(Succeed())

			pool, err := startPool(folder, false)
			Expect(pool).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("collection_100")))
			Expect(err).To(MatchError(ContainSubstring("apply migrations offline or enable --auto-migrate")))
			Expect(stale).To(BeAnExistingFile())
			db = openStartupDatabase(folder, "collection_100.duckdb")
			var version, columns int
			Expect(db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version)).To(Succeed())
			Expect(version).To(Equal(1))
			Expect(db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_name='vdisk' AND column_name='RDM LUN UUID'`).Scan(&columns)).To(Succeed())
			Expect(columns).To(BeZero())
			Expect(db.Close()).To(Succeed())
			db = openStartupDatabase(folder, "agent.duckdb")
			var markers int
			Expect(db.QueryRow(`SELECT count(*) FROM collections`).Scan(&markers)).To(Succeed())
			Expect(markers).To(Equal(1))
		})

		It("upgrades existing collections when automatic migration is enabled", func() {
			seedStartupCollection(folder)
			pool, err := startPool(folder, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(pool.List()).To(HaveLen(2))
			pool.Close()
			db := openStartupDatabase(folder, "collection_100.duckdb")
			Expect(migrations.ValidateCollection(context.Background(), db, "collection_100")).To(Succeed())
		})

		It("excludes a failed collection migration without deleting its file", func() {
			seedStartupCollection(folder)
			corrupt := filepath.Join(folder, "collection_200.duckdb")
			Expect(os.WriteFile(corrupt, []byte("corrupt database"), 0600)).To(Succeed())

			pool, err := startPool(folder, true)
			Expect(err).NotTo(HaveOccurred())
			defer pool.Close()
			Expect(pool.List()).To(HaveLen(2))
			latest, err := pool.Latest()
			Expect(err).NotTo(HaveOccurred())
			Expect(latest.Path).To(Equal(filepath.Join(folder, "collection_100.duckdb")))
			contents, err := os.ReadFile(corrupt)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contents)).To(Equal("corrupt database"))
		})

		It("does not register a stale collection deleted after validation", func() {
			seedStartupCollection(folder)
			db := openStartupDatabase(folder, "collection_100.duckdb")
			Expect(migrations.RunCollection(context.Background(), db, "collection_100")).To(Succeed())
			Expect(db.Close()).To(Succeed())
			db = openStartupDatabase(folder, "agent.duckdb")
			_, err := db.Exec(`INSERT INTO collections ("database",state) VALUES ('collection_100','failed')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())
			pool, err := startPool(folder, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(pool.List()).To(HaveLen(1))
			pool.Close()
			_, err = os.Stat(filepath.Join(folder, "collection_100.duckdb"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		DescribeTable("fails on unreadable or uninitialized collections instead of skipping them",
			func(contents []byte) {
				Expect(os.WriteFile(filepath.Join(folder, "collection_100.duckdb"), contents, 0600)).To(Succeed())
				pool, err := startPool(folder, false)
				Expect(pool).To(BeNil())
				Expect(err).To(MatchError(ContainSubstring("failed to validate collection database collection_100")))
			},
			Entry("unreadable file", []byte("corrupt database")),
			Entry("uninitialized database", []byte{}),
		)
	})
})

type startupMigrationEntry struct {
	Version   int
	AppliedAt time.Time
}

func readStartupMainHistory(folder string) []startupMigrationEntry {
	db := openStartupDatabase(folder, "agent.duckdb")
	rows, err := db.Query(`SELECT version, applied_at FROM schema_migrations ORDER BY version`)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = rows.Close() }()

	var history []startupMigrationEntry
	for rows.Next() {
		var entry startupMigrationEntry
		Expect(rows.Scan(&entry.Version, &entry.AppliedAt)).To(Succeed())
		history = append(history, entry)
	}
	Expect(rows.Err()).NotTo(HaveOccurred())
	Expect(rows.Close()).To(Succeed())
	Expect(db.Close()).To(Succeed())
	return history
}

func readStartupCollectionHistory(folder string) []startupMigrationEntry {
	db := openStartupDatabase(folder, "collection_100.duckdb")
	rows, err := db.Query(`SELECT version, applied_at FROM collection_schema_migrations ORDER BY version`)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = rows.Close() }()

	var history []startupMigrationEntry
	for rows.Next() {
		var entry startupMigrationEntry
		Expect(rows.Scan(&entry.Version, &entry.AppliedAt)).To(Succeed())
		history = append(history, entry)
	}
	Expect(rows.Err()).NotTo(HaveOccurred())
	Expect(rows.Close()).To(Succeed())
	Expect(db.Close()).To(Succeed())
	return history
}

func openStartupDatabase(folder, name string) *sql.DB {
	db, err := sql.Open("duckdb", filepath.Join(folder, name))
	Expect(err).NotTo(HaveOccurred())
	db.SetMaxOpenConns(1)
	DeferCleanup(db.Close)
	return db
}

func seedStartupCollection(folder string) {
	db := openStartupDatabase(folder, "collection_100.duckdb")
	_, err := db.Exec(`CREATE TABLE collection_schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT now());
		INSERT INTO collection_schema_migrations(version) VALUES (1);
		CREATE TABLE vdisk ("VM ID" VARCHAR);
		CREATE TABLE vdatastore ("Name" VARCHAR);
		CREATE TABLE vm_applications (
			app_name VARCHAR NOT NULL,
			app_desc VARCHAR NOT NULL,
			vm_id VARCHAR NOT NULL,
			vm_name VARCHAR NOT NULL,
			PRIMARY KEY (app_name, vm_id)
		);`)
	Expect(err).NotTo(HaveOccurred())
	Expect(db.Close()).To(Succeed())
}

func startPool(folder string, autoMigrate bool) (*store.Pool, error) {
	pool, err := bootstrap.InitPool(folder)
	if err != nil {
		return nil, err
	}
	if autoMigrate {
		err = pool.Migrate(context.Background())
	} else {
		err = pool.ValidateMigrations(context.Background())
	}
	if err == nil {
		err = bootstrap.CleanupStaleCollections(context.Background(), pool, folder)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
