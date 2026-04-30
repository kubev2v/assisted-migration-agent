package store_test

import (
	"context"
	"database/sql"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore Migration Exclusion", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
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

	Context("GetUserInfo", func() {
		// Given a VM exists with migration_excluded = true
		// When GetUserInfo is called
		// Then it should return VMUserInfo with MigrationExcluded = true
		It("should return VMUserInfo with MigrationExcluded = true for excluded VM", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateMigrationExcluded(ctx, "vm-1", true)
			Expect(err).NotTo(HaveOccurred())

			// Act
			userInfo, err := s.VM().GetUserInfo(ctx, "vm-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(userInfo).NotTo(BeNil())
			Expect(userInfo.VMID).To(Equal("vm-1"))
			Expect(userInfo.MigrationExcluded).To(BeTrue())
		})

		// Given a VM exists with migration_excluded = false
		// When GetUserInfo is called
		// Then it should return VMUserInfo with MigrationExcluded = false
		It("should return VMUserInfo with MigrationExcluded = false for included VM", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateMigrationExcluded(ctx, "vm-2", false)
			Expect(err).NotTo(HaveOccurred())

			// Act
			userInfo, err := s.VM().GetUserInfo(ctx, "vm-2")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(userInfo).NotTo(BeNil())
			Expect(userInfo.VMID).To(Equal("vm-2"))
			Expect(userInfo.MigrationExcluded).To(BeFalse())
		})

		// Given a VM exists but has no entry in vm_user_info
		// When GetUserInfo is called
		// Then it should return VMUserInfo with default values
		It("should return default values for VM without vm_user_info entry", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")

			// Act
			userInfo, err := s.VM().GetUserInfo(ctx, "vm-3")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(userInfo).NotTo(BeNil())
			Expect(userInfo.VMID).To(Equal("vm-3"))
			Expect(userInfo.MigrationExcluded).To(BeFalse())
		})

		// Given a VM ID that doesn't exist
		// When GetUserInfo is called
		// Then it should return VMUserInfo with default values (COALESCE)
		It("should return default values for non-existent VM", func() {
			// Act
			userInfo, err := s.VM().GetUserInfo(ctx, "non-existent-vm")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(userInfo).NotTo(BeNil())
			Expect(userInfo.VMID).To(Equal("non-existent-vm"))
			Expect(userInfo.MigrationExcluded).To(BeFalse())
		})
	})

	Context("UpdateMigrationExcluded", func() {
		// Given a VM exists in the database
		// When UpdateMigrationExcluded is called to exclude it
		// Then the VM should be marked as excluded
		It("should successfully exclude a VM", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")

			// Act
			err := s.VM().UpdateMigrationExcluded(ctx, "vm-1", true)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify the flag was set
			var excluded bool
			err = db.QueryRowContext(ctx, `SELECT migration_excluded FROM vm_user_info WHERE "VM ID" = ?`, "vm-1").Scan(&excluded)
			Expect(err).NotTo(HaveOccurred())
			Expect(excluded).To(BeTrue())
		})

		// Given a VM exists and is excluded
		// When UpdateMigrationExcluded is called to include it
		// Then the VM should be marked as included
		It("should successfully include a previously excluded VM", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateMigrationExcluded(ctx, "vm-2", true)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().UpdateMigrationExcluded(ctx, "vm-2", false)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify the flag was updated
			var excluded bool
			err = db.QueryRowContext(ctx, `SELECT migration_excluded FROM vm_user_info WHERE "VM ID" = ?`, "vm-2").Scan(&excluded)
			Expect(err).NotTo(HaveOccurred())
			Expect(excluded).To(BeFalse())
		})

		// Given a VM exists and has been excluded twice
		// When we check the database
		// Then there should be only one record (UPSERT behavior)
		It("should use UPSERT pattern to avoid duplicate records", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")

			// Act - call twice
			err := s.VM().UpdateMigrationExcluded(ctx, "vm-3", true)
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateMigrationExcluded(ctx, "vm-3", true)
			Expect(err).NotTo(HaveOccurred())

			// Assert - verify only one record exists
			var count int
			err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vm_user_info WHERE "VM ID" = ?`, "vm-3").Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		// Given a VM ID that doesn't exist in vinfo
		// When UpdateMigrationExcluded is called
		// Then it should fail due to foreign key constraint
		It("should fail for non-existent VM due to foreign key constraint", func() {
			// Act
			err := s.VM().UpdateMigrationExcluded(ctx, "non-existent-vm", true)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("foreign key constraint"))
		})
	})

	Context("List with migration_excluded field", func() {
		BeforeEach(func() {
			// Insert test VMs
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-b")
			insertVM("vm-4", "VM 4", "cluster-b")

			// Exclude VM 2 and VM 4
			Expect(s.VM().UpdateMigrationExcluded(ctx, "vm-2", true)).To(Succeed())
			Expect(s.VM().UpdateMigrationExcluded(ctx, "vm-4", true)).To(Succeed())
		})

		// Given VMs with mixed exclusion status
		// When listing all VMs
		// Then all VMs should be returned with correct migration_excluded values
		It("should return migration_excluded field for all VMs", func() {
			// Act
			vms, err := s.VM().List(ctx, nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(4))

			// Create a map for easier assertions
			vmMap := make(map[string]bool)
			for _, vm := range vms {
				vmMap[vm.ID] = vm.MigrationExcluded
			}

			Expect(vmMap["vm-1"]).To(BeFalse(), "vm-1 should not be excluded")
			Expect(vmMap["vm-2"]).To(BeTrue(), "vm-2 should be excluded")
			Expect(vmMap["vm-3"]).To(BeFalse(), "vm-3 should not be excluded")
			Expect(vmMap["vm-4"]).To(BeTrue(), "vm-4 should be excluded")
		})

		// Given VMs where some have no entry in vm_user_info
		// When listing all VMs
		// Then VMs without vm_user_info should default to migration_excluded = false
		It("should default to false for VMs without vm_user_info entry", func() {
			// Arrange - add a new VM without setting exclusion status
			insertVM("vm-5", "VM 5", "cluster-c")

			// Act
			vms, err := s.VM().List(ctx, nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			vm5Result := findVM(vms, "vm-5")
			Expect(vm5Result).NotTo(BeNil())
			vm5 := vm5Result.(*models.VirtualMachineSummary)
			Expect(vm5.MigrationExcluded).To(BeFalse())
		})

		// Given VMs with exclusion status
		// When filtering by migration_excluded = true
		// Then only excluded VMs should be returned
		It("should filter by migration_excluded = true", func() {
			// Arrange
			filter := sq.Eq{`COALESCE(vui.migration_excluded, FALSE)`: true}

			// Act
			vms, err := s.VM().List(ctx, []sq.Sqlizer{filter})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))
			Expect(vms[0].MigrationExcluded).To(BeTrue())
			Expect(vms[1].MigrationExcluded).To(BeTrue())

			ids := []string{vms[0].ID, vms[1].ID}
			Expect(ids).To(ConsistOf("vm-2", "vm-4"))
		})

		// Given VMs with exclusion status
		// When filtering by migration_excluded = false
		// Then only included VMs should be returned
		It("should filter by migration_excluded = false", func() {
			// Arrange
			filter := sq.Eq{`COALESCE(vui.migration_excluded, FALSE)`: false}

			// Act
			vms, err := s.VM().List(ctx, []sq.Sqlizer{filter})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(2))
			Expect(vms[0].MigrationExcluded).To(BeFalse())
			Expect(vms[1].MigrationExcluded).To(BeFalse())

			ids := []string{vms[0].ID, vms[1].ID}
			Expect(ids).To(ConsistOf("vm-1", "vm-3"))
		})

		// Given VMs with exclusion status
		// When filtering by both cluster and migration_excluded
		// Then only VMs matching both criteria should be returned
		It("should support combining migration_excluded filter with other filters", func() {
			// Arrange - filter for cluster-b AND not excluded
			clusterFilter := sq.Eq{`v."Cluster"`: "cluster-b"}
			excludedFilter := sq.Eq{`COALESCE(vui.migration_excluded, FALSE)`: false}

			// Act
			vms, err := s.VM().List(ctx, []sq.Sqlizer{clusterFilter, excludedFilter})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].ID).To(Equal("vm-3"))
			Expect(vms[0].Cluster).To(Equal("cluster-b"))
			Expect(vms[0].MigrationExcluded).To(BeFalse())
		})
	})

	Context("Count with migration_excluded filter", func() {
		BeforeEach(func() {
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			Expect(s.VM().UpdateMigrationExcluded(ctx, "vm-2", true)).To(Succeed())
		})

		// Given VMs with exclusion status
		// When counting excluded VMs
		// Then the count should be correct
		It("should count excluded VMs correctly", func() {
			// Arrange
			filter := sq.Eq{`COALESCE(vui.migration_excluded, FALSE)`: true}

			// Act
			count, err := s.VM().Count(ctx, filter)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		// Given VMs with exclusion status
		// When counting included VMs
		// Then the count should be correct
		It("should count included VMs correctly", func() {
			// Arrange
			filter := sq.Eq{`COALESCE(vui.migration_excluded, FALSE)`: false}

			// Act
			count, err := s.VM().Count(ctx, filter)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})
	})
})

// Helper function to find a VM by ID in a slice
func findVM(vms interface{}, id string) interface{} {
	switch v := vms.(type) {
	case []models.VirtualMachineSummary:
		for i := range v {
			if v[i].ID == id {
				return &v[i]
			}
		}
	}
	return nil
}
