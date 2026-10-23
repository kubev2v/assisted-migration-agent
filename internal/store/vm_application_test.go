package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore Application Methods", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "vm-application-store-test-*")
		Expect(err).NotTo(HaveOccurred())
		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		s, err = collDB.Store()
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

	Describe("GetGuestApps", func() {
		insertVMWithApps := func(id, name, guestAppsJSON string) {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "guest_apps")
				VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, ?)
			`, id, name, guestAppsJSON)
			Expect(err).NotTo(HaveOccurred())
		}

		It("should return VMs with parsed guest app names", func() {
			insertVMWithApps("vm-1", "db-01", `[{"name":"postgres"},{"name":"nginx"}]`)
			insertVMWithApps("vm-2", "web-01", `[{"name":"apache"}]`)

			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))

			// Find vm-1
			var vm1 models.VMGuestApps
			for _, r := range result {
				if r.ID == "vm-1" {
					vm1 = r
					break
				}
			}
			Expect(vm1.Name).To(Equal("db-01"))
			Expect(vm1.AppNames).To(ConsistOf("postgres", "nginx"))
		})

		It("ingests vApp products and returns them in VM application and detail views", func() {
			insertVMWithApps("vm-1", "appliance", `[]`)
			insertVMWithApps("vm-2", "plain-vm", `[]`)

			sqlitePath := filepath.Join(tmpDir, "forklift.db")
			escapedPath := strings.ReplaceAll(sqlitePath, "'", "''")
			_, err := s.Querier().ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS product_fixture (TYPE sqlite)", escapedPath))
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Querier().ExecContext(ctx, `CREATE TABLE product_fixture.VM ("ID" VARCHAR, "VAppProducts" VARCHAR)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Querier().ExecContext(ctx, `INSERT INTO product_fixture.VM VALUES (?, ?), (?, ?)`,
				"vm-1", `[{"key":7,"classId":"com.example.datasync","instanceId":"datasync-01","name":"DataSync","vendor":"AWS","version":"1.2"},{"key":8,"name":"Other Product","vendor":"Example"}]`,
				"vm-2", `[{"key":0}]`)
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Querier().ExecContext(ctx, `DETACH product_fixture`)
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Application().InsertProducts(ctx, sqlitePath)).To(Succeed())

			var count int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM vm_vapp_products`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(2))

			vms, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))
			byID := make(map[string]models.VMGuestApps, len(vms))
			for _, vm := range vms {
				byID[vm.ID] = vm
			}
			Expect(byID["vm-1"].Products).To(ConsistOf(
				models.AppProduct{Key: 7, ClassId: "com.example.datasync", InstanceId: "datasync-01", Name: "DataSync", Vendor: "AWS", Version: "1.2"},
				models.AppProduct{Key: 8, Name: "Other Product", Vendor: "Example"},
			))
			Expect(byID["vm-2"].Products).To(BeEmpty())

			detail, err := s.VM().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(detail.AppProducts).To(ConsistOf(byID["vm-1"].Products[0], byID["vm-1"].Products[1]))
		})

		It("should return empty app names for VMs with no guest apps", func() {
			insertVMWithApps("vm-1", "empty-vm", `[]`)

			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].AppNames).To(BeEmpty())
		})

		It("should handle null guest_apps via COALESCE", func() {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
				VALUES ('vm-1', 'null-vm', 'poweredOn', 'cluster-a', 4096, false)
			`)
			Expect(err).NotTo(HaveOccurred())

			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].AppNames).To(BeEmpty())
		})

		It("should filter out entries with empty names", func() {
			insertVMWithApps("vm-1", "mixed-vm", `[{"name":"postgres"},{"name":""},{"name":"nginx"}]`)

			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].AppNames).To(ConsistOf("postgres", "nginx"))
		})

		It("should return empty result for empty table", func() {
			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})

	Describe("GetFilterOptions includes applications", func() {
		It("should return distinct application names from vm_applications", func() {
			// Insert VMs for FK constraints
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
				VALUES ('vm-1', 'vm-one', 'poweredOn', 'cluster-a', 4096, false)
			`)
			Expect(err).NotTo(HaveOccurred())

			// Populate vm_applications
			err = s.Application().ReplaceAll(ctx, []models.ApplicationVMRecord{
				{AppName: "PostgreSQL", AppDesc: "PG", VMID: "vm-1", VMName: "vm-one"},
				{AppName: "Apache", AppDesc: "Web", VMID: "vm-1", VMName: "vm-one"},
			})
			Expect(err).NotTo(HaveOccurred())

			opts, err := s.VM().GetFilterOptions(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(opts.Applications).To(ConsistOf("Apache", "PostgreSQL"))
		})

		It("should return empty applications when vm_applications is empty", func() {
			opts, err := s.VM().GetFilterOptions(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(opts.Applications).To(BeEmpty())
		})
	})
})
