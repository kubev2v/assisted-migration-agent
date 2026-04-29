package services_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMService Migration Exclusion", func() {
	var (
		ctx context.Context
		svc *services.VMService
		st  *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		svc = services.NewVMService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	// Helper to insert VM into vinfo table
	insertVM := func(id, name, cluster string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, 'poweredOn', ?, 4096, false)
		`, id, name, cluster)
		Expect(err).NotTo(HaveOccurred())
	}

	Context("UpdateMigrationExcluded", func() {
		// Given a VM exists in the database
		// When UpdateMigrationExcluded is called
		// Then the VM should be marked as excluded
		It("should successfully exclude a VM", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")

			// Act
			err := svc.UpdateMigrationExcluded(ctx, "vm-1", true)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify via List
			params := services.VMListParams{
				Expression: "migration_excluded = true",
			}
			vms, _, err := svc.List(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].ID).To(Equal("vm-1"))
			Expect(vms[0].MigrationExcluded).To(BeTrue())

			// Verify via Get
			vm, err := svc.Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.MigrationExcluded).To(BeTrue())
		})

		// Given a VM exists and is excluded
		// When UpdateMigrationExcluded is called with false
		// Then the VM should be marked as included
		It("should successfully include a previously excluded VM", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := svc.UpdateMigrationExcluded(ctx, "vm-2", true)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateMigrationExcluded(ctx, "vm-2", false)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify via List
			params := services.VMListParams{
				Expression: "migration_excluded = false",
			}
			vms, _, err := svc.List(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].ID).To(Equal("vm-2"))
			Expect(vms[0].MigrationExcluded).To(BeFalse())

			// Verify via Get
			vm, err := svc.Get(ctx, "vm-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.MigrationExcluded).To(BeFalse())
		})

		// Given a VM ID that doesn't exist
		// When UpdateMigrationExcluded is called
		// Then it should return a ResourceNotFoundError
		It("should return ResourceNotFoundError for non-existent VM", func() {
			// Act
			err := svc.UpdateMigrationExcluded(ctx, "non-existent-vm", true)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Context("List with MigrationExcluded filter", func() {
		BeforeEach(func() {
			// Insert test VMs
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-b")
			insertVM("vm-4", "VM 4", "cluster-b")

			// Exclude VM 2 and VM 4
			Expect(svc.UpdateMigrationExcluded(ctx, "vm-2", true)).To(Succeed())
			Expect(svc.UpdateMigrationExcluded(ctx, "vm-4", true)).To(Succeed())
		})

		// Given VMs with mixed exclusion status
		// When listing with MigrationExcluded = nil (no filter)
		// Then all VMs should be returned
		It("should return all VMs when MigrationExcluded filter is not set", func() {
			// Arrange
			params := services.VMListParams{}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(4))
			Expect(total).To(Equal(4))
		})

		// Given VMs with mixed exclusion status
		// When listing with MigrationExcluded = true
		// Then only excluded VMs should be returned
		It("should return only excluded VMs when MigrationExcluded = true", func() {
			// Arrange
			params := services.VMListParams{
				Expression: "migration_excluded = true",
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))
			Expect(total).To(Equal(2))

			ids := []string{vms[0].ID, vms[1].ID}
			Expect(ids).To(ConsistOf("vm-2", "vm-4"))

			for _, vm := range vms {
				Expect(vm.MigrationExcluded).To(BeTrue())
			}
		})

		// Given VMs with mixed exclusion status
		// When listing with MigrationExcluded = false
		// Then only included VMs should be returned
		It("should return only included VMs when MigrationExcluded = false", func() {
			// Arrange
			params := services.VMListParams{
				Expression: "migration_excluded = false",
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))
			Expect(total).To(Equal(2))

			ids := []string{vms[0].ID, vms[1].ID}
			Expect(ids).To(ConsistOf("vm-1", "vm-3"))

			for _, vm := range vms {
				Expect(vm.MigrationExcluded).To(BeFalse())
			}
		})

		// Given VMs with mixed exclusion status
		// When combining MigrationExcluded filter with Expression filter
		// Then both filters should be applied
		It("should combine MigrationExcluded filter with Expression filter", func() {
			// Arrange - filter for cluster-a AND excluded
			params := services.VMListParams{
				Expression: `cluster = "cluster-a" and migration_excluded = true`,
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(total).To(Equal(1))
			Expect(vms[0].ID).To(Equal("vm-2"))
			Expect(vms[0].Cluster).To(Equal("cluster-a"))
			Expect(vms[0].MigrationExcluded).To(BeTrue())
		})

		// Given VMs with pagination parameters
		// When listing excluded VMs with limit
		// Then pagination should work correctly
		It("should support pagination with MigrationExcluded filter", func() {
			// Arrange
			params := services.VMListParams{
				Expression: "migration_excluded = true",
				Limit:      1,
				Offset:     0,
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(total).To(Equal(2)) // Total should still be 2

			// Get second page
			params.Offset = 1
			vms2, total2, err := svc.List(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms2).To(HaveLen(1))
			Expect(total2).To(Equal(2))

			// Ensure we got different VMs
			Expect(vms[0].ID).NotTo(Equal(vms2[0].ID))
		})
	})

	Context("Edge cases", func() {
		// Given no VMs exist
		// When listing with MigrationExcluded filter
		// Then empty list should be returned
		It("should return empty list when no VMs match the filter", func() {
			// Arrange
			params := services.VMListParams{
				Expression: "migration_excluded = true",
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(BeEmpty())
			Expect(total).To(Equal(0))
		})

		// Given all VMs are excluded
		// When listing with MigrationExcluded = false
		// Then empty list should be returned
		It("should return empty list when all VMs are excluded but filtering for included", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			Expect(svc.UpdateMigrationExcluded(ctx, "vm-1", true)).To(Succeed())
			Expect(svc.UpdateMigrationExcluded(ctx, "vm-2", true)).To(Succeed())

			params := services.VMListParams{
				Expression: "migration_excluded = false",
			}

			// Act
			vms, total, err := svc.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(BeEmpty())
			Expect(total).To(Equal(0))
		})

		// Given a VM is toggled between excluded and included multiple times
		// When checking its status
		// Then it should reflect the latest state
		It("should handle toggling exclusion status multiple times", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")

			// Act & Assert - toggle multiple times
			Expect(svc.UpdateMigrationExcluded(ctx, "vm-1", true)).To(Succeed())
			vms, _, _ := svc.List(ctx, services.VMListParams{Expression: "migration_excluded = true"})
			Expect(vms).To(HaveLen(1))

			Expect(svc.UpdateMigrationExcluded(ctx, "vm-1", false)).To(Succeed())
			vms, _, _ = svc.List(ctx, services.VMListParams{Expression: "migration_excluded = true"})
			Expect(vms).To(BeEmpty())

			Expect(svc.UpdateMigrationExcluded(ctx, "vm-1", true)).To(Succeed())
			vms, _, _ = svc.List(ctx, services.VMListParams{Expression: "migration_excluded = true"})
			Expect(vms).To(HaveLen(1))
		})
	})
})
