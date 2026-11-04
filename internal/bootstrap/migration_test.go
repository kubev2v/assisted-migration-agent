package bootstrap_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

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

			pool, err := startPool(folder, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(pool.List()).To(HaveLen(2))
			pool.Close()
			db = openStartupDatabase(folder, "collection_100.duckdb")
			var unchanged int
			Expect(db.QueryRow(`SELECT count(*) FROM collection_schema_migrations WHERE applied_at='2000-01-01'`).Scan(&unchanged)).To(Succeed())
			Expect(unchanged).To(Equal(2))
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
		CREATE TABLE vdatastore ("Name" VARCHAR);`)
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
