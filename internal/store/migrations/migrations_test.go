package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx context.Context
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Context("Run", func() {
		It("should run all migrations successfully", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create configuration table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Verify configuration table exists by inserting data
			_, err = db.ExecContext(ctx, `
				INSERT INTO configuration (id, agent_mode)
				VALUES (1, 'disconnected')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create inventory table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Verify inventory table exists by inserting data
			_, err = db.ExecContext(ctx, `
				INSERT INTO inventory (id, data)
				VALUES (1, 'test data')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be idempotent", func() {
			// Run migrations twice
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			err = migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied migrations in schema_migrations table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Verify schema_migrations table has entries
			rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()

			var versions []int
			for rows.Next() {
				var v int
				err := rows.Scan(&v)
				Expect(err).NotTo(HaveOccurred())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())

			Expect(versions).To(ContainElements(1))
		})

		// Given migrations have been applied
		// When we check the version ordering
		// Then versions should be sequential starting from 1
		It("should apply migrations in sequential order", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())

			// Versions should be sequential
			for i, v := range versions {
				Expect(v).To(Equal(i + 1))
			}
		})

		// Given migrations have been applied
		// When we check the vm_inspection_status table
		// Then it should exist and accept inserts
		It("should create vm_inspection_status table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Insert a row into vinfo first (FK constraint)
			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM") VALUES ('vm-1', 'test-vm')
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vm_inspection_status ("VM ID", status)
				VALUES ('vm-1', 'pending')
			`)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
