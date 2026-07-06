package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx    context.Context
		db     *sql.DB
		st     *store.Store
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "migrations-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx, "")).To(Succeed())
		Expect(st.CreateDatabase(ctx, tmpDir, "collection_test")).To(Succeed())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Main migrations", func() {
		It("should create configuration table", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO configuration (id, agent_mode)
				VALUES (1, 'disconnected')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add skip_tls and ca_cert columns to credentials table", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO credentials (id, url, username, password, skip_tls, ca_cert)
				VALUES ('test-tls', 'https://vc.local', 'u', 'p', true, 'cert')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create collections table", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO collections ("database", state)
				VALUES ('collection_default', 'running')
			`)
			Expect(err).NotTo(HaveOccurred())

			var ns string
			err = db.QueryRowContext(ctx, `SELECT "database" FROM collections WHERE "database" = 'collection_default'`).Scan(&ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal("collection_default"))
		})

		It("should track applied main migrations", func() {
			rows, err := db.QueryContext(ctx, `SELECT version FROM agent.main.schema_migrations ORDER BY version`)
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
		BeforeEach(func() {
			_, err := db.ExecContext(ctx, `USE collection_test`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create inventory table", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO inventory (id, data)
				VALUES (1, 'test data')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create vm_inspection_status table", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM") VALUES ('vm-1', 'test-vm')
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vm_inspection_status ("VM ID", status)
				VALUES ('vm-1', 'pending')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied collection migrations", func() {
			rows, err := db.QueryContext(ctx, `SELECT version FROM collection_test.main.collection_schema_migrations ORDER BY version`)
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
			Expect(st.Migrate(ctx, "")).To(Succeed())
		})

		It("should be safe to init collection twice", func() {
			_, err := db.ExecContext(ctx, `USE collection_test`)
			Expect(err).NotTo(HaveOccurred())
			Expect(st.InitCollection(ctx)).To(Succeed())
		})
	})
})
