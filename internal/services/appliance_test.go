package services_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("ApplianceService", Ordered, func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		st     *store.Store
		sqlDB  *sql.DB
		srv    *services.ApplianceService
		tmpDir string
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-service-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		mainSt, err := mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		rulesSrv, err := services.NewApplianceRulesService(mainSt)
		Expect(err).NotTo(HaveOccurred())

		dbPath := filepath.Join(tmpDir, "collection.duckdb")
		db, err := pool.NewDatabase("collection", dbPath, time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		Expect(db.Migrate(ctx, func(ctx context.Context, d *sql.DB) error {
			sqlDB = d
			s, err := db.Store()
			if err != nil {
				return err
			}
			if err := duckdb_parser.New(s.Querier(), nil).Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, d, "collection")
		})).To(Succeed())

		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())

		srv, err = services.NewApplianceService(st, rulesSrv)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Given a collection seeded with vApp products", func() {
		BeforeEach(func() {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "guest_apps")
				VALUES
					('vm-1', 'edge-01', 'poweredOn', 'cluster-a', 4096, false, '[]'),
					('vm-2', 'edge-02', 'poweredOn', 'cluster-a', 4096, false, '[]')
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = sqlDB.ExecContext(ctx, `
				INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
				VALUES
					('vm-1', 1, 'DataSync', 'AWS', '2.0'),
					('vm-2', 1, 'BIG-IP', 'F5', '17.1')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			_, err := sqlDB.ExecContext(ctx, `DELETE FROM vm_vapp_products`)
			Expect(err).NotTo(HaveOccurred())

			_, err = sqlDB.ExecContext(ctx, `DELETE FROM vinfo`)
			Expect(err).NotTo(HaveOccurred())
		})

		When("appliances are listed without a filter", func() {
			// Given two VMs, each with one vApp product
			// When the appliance list is requested without a filter
			// Then both appliances are returned, ordered by name
			It("should return all grouped appliances", func() {
				// Act
				appliances, err := srv.List(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(2))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
				Expect(appliances[1].Name).To(Equal("DataSync"))
			})
		})

		When("appliances are listed with a vm_id expression", func() {
			// Given two VMs with different appliances
			// When the list is filtered by the id of the first VM
			// Then only the appliance of that VM is returned
			It("should return only appliances on that VM", func() {
				// Act
				appliances, err := srv.List(ctx, "vm_id in ['vm-1']")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("DataSync"))
			})
		})

		When("appliances are listed with a vm_name expression", func() {
			// Given two VMs with different appliances
			// When the list is filtered by the name of the second VM
			// Then only the appliance of that VM is returned
			It("should return only appliances on that VM", func() {
				// Act
				appliances, err := srv.List(ctx, "vm_name = 'edge-02'")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})

		When("appliances are listed with a name expression", func() {
			// Given two appliances
			// When the list is filtered by one appliance name
			// Then only that appliance is returned
			It("should return only the matching appliance", func() {
				// Act
				appliances, err := srv.List(ctx, "name = 'DataSync'")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("DataSync"))
			})
		})

		When("appliances are listed with a vendor expression", func() {
			// Given two appliances from different vendors
			// When the list is filtered by one vendor
			// Then only the appliance of that vendor is returned
			It("should return only appliances from that vendor", func() {
				// Act
				appliances, err := srv.List(ctx, "vendor = 'F5'")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})

		When("appliances are listed with a version expression", func() {
			// Given two appliances with different versions
			// When the list is filtered by one version
			// Then only the appliance of that version is returned
			It("should return only appliances with that version", func() {
				// Act
				appliances, err := srv.List(ctx, "version = '17.1'")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})

		When("appliances are listed with a combined expression", func() {
			// Given two appliances
			// When the list is filtered by vendor and version at once
			// Then only the appliance that satisfies both is returned
			It("should return only appliances matching both conditions", func() {
				// Act
				appliances, err := srv.List(ctx, "vendor = 'AWS' and version = '2.0'")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("DataSync"))
			})
		})
	})

	When("the collection has no appliances", func() {
		// Given a collection without any vApp product
		// When the appliance list is requested
		// Then the list is empty and no error is returned
		It("should return an empty list", func() {
			// Act
			appliances, err := srv.List(ctx, "")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(appliances).To(BeEmpty())
		})
	})

	When("the filter expression is invalid", func() {
		// Given a collection
		// When the list is filtered by an attribute the appliance model does not have
		// Then an invalid filter error is returned
		It("should return an invalid filter error", func() {
			// Act
			_, err := srv.List(ctx, "invalid_field = 'x'")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInvalidFilterError(err)).To(BeTrue())
		})
	})
})
