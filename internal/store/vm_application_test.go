package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore Application Methods", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error

		db, err = store.NewConnection(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())

		Expect(s.InitCollection(ctx)).To(Succeed())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Describe("GetGuestApps", func() {
		insertVMWithApps := func(id, name, guestAppsJSON string) {
			_, err := db.ExecContext(ctx, `
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

		It("should return empty app names for VMs with no guest apps", func() {
			insertVMWithApps("vm-1", "empty-vm", `[]`)

			result, err := s.VM().GetGuestApps(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].AppNames).To(BeEmpty())
		})

		It("should handle null guest_apps via COALESCE", func() {
			_, err := db.ExecContext(ctx, `
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
			_, err := db.ExecContext(ctx, `
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
