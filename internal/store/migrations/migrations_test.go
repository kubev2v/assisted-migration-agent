package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		mainSt *store.Store
		collSt *store.Store
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "migrations-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		mainSt, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		collDB, err := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), nil).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		collSt, err = collDB.Store()
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

	Context("Main migrations", func() {
		It("should create configuration table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO configuration (id, agent_mode)
				VALUES (1, 'disconnected')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add skip_tls and ca_cert columns to credentials table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO credentials (id, url, username, password, skip_tls, ca_cert)
				VALUES ('test-tls', 'https://vc.local', 'u', 'p', true, 'cert')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create collections table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO collections ("database", state)
				VALUES ('collection_default', 'running')
			`)
			Expect(err).NotTo(HaveOccurred())

			var ns string
			err = mainSt.Querier().QueryRowContext(ctx, `SELECT "database" FROM collections WHERE "database" = 'collection_default'`).Scan(&ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal("collection_default"))
		})

		It("should track applied main migrations", func() {
			rows, err := mainSt.Querier().QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))

			for i, v := range versions {
				Expect(v).To(Equal(i + 1))
			}
		})
	})

	Context("Collection migrations", func() {
		It("should create inventory table", func() {
			_, err := collSt.Querier().ExecContext(ctx, `
				INSERT INTO inventory (id, data)
				VALUES (1, 'test data')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied collection migrations", func() {
			rows, err := collSt.Querier().QueryContext(ctx, `SELECT version FROM collection_schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))
		})
	})

	Context("Idempotency", func() {
		It("should be safe to run main migrations twice", func() {
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		})

		It("should be safe to run collection migrations twice", func() {
			collDB, err := pool.Get("coll")
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
		})
	})
})
