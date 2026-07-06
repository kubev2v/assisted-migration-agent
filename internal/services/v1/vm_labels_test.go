package v1_test

import (
	"context"
	"database/sql"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMService Labels", func() {
	var (
		ctx context.Context
		svc *services.VMService
		st  *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewConnection(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.InitCollection(ctx)).To(Succeed())
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

	Context("UpdateLabels", func() {
		// Given a VM exists in the database
		// When UpdateLabels is called with valid labels
		// Then the VM should have those labels
		It("should successfully set labels on a VM", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")

			// Act
			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "critical"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify via List
			params := services.VMListParams{}
			vms, _, err := svc.List(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production", "critical"}))

			// Verify via Get
			vm, err := svc.Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(Equal([]string{"production", "critical"}))
		})

		// Given a VM exists with labels
		// When UpdateLabels is called with different labels
		// Then labels should be replaced (not appended)
		It("should replace labels (not append)", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-2", []string{"old-label"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateLabels(ctx, "vm-2", []string{"new-label"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm, err := svc.Get(ctx, "vm-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(Equal([]string{"new-label"}))
			Expect(vm.Labels).NotTo(ContainElement("old-label"))
		})

		// Given a VM exists with labels
		// When UpdateLabels is called with empty array
		// Then labels should be cleared
		It("should successfully clear labels with empty array", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-3", []string{"label1", "label2"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateLabels(ctx, "vm-3", []string{})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm, err := svc.Get(ctx, "vm-3")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(BeEmpty())
		})

		// Given a VM exists with labels
		// When UpdateLabels is called with nil
		// Then labels should be cleared (nil treated as empty array)
		It("should handle nil labels as empty array", func() {
			// Arrange
			insertVM("vm-4", "Test VM 4", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-4", []string{"label1"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateLabels(ctx, "vm-4", nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm, err := svc.Get(ctx, "vm-4")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(BeEmpty())
		})

		// Given a VM ID that doesn't exist
		// When UpdateLabels is called
		// Then it should return an error
		It("should return error for non-existent VM", func() {
			// Act
			err := svc.UpdateLabels(ctx, "non-existent-vm", []string{"label"})

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("UpdateLabels validation", func() {
		BeforeEach(func() {
			insertVM("vm-1", "Test VM 1", "cluster-a")
		})

		// Given a label is exactly 100 characters
		// When UpdateLabels is called
		// Then it should succeed
		It("should accept labels with exactly 100 characters", func() {
			// Arrange - create a 100 character label
			maxLabel := strings.Repeat("a", 100)

			// Act
			err := svc.UpdateLabels(ctx, "vm-1", []string{maxLabel})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm, err := svc.Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(HaveLen(1))
			Expect(vm.Labels[0]).To(Equal(maxLabel))
		})

		// Given labels contain special characters
		// When UpdateLabels is called
		// Then it should succeed (special chars are allowed)
		It("should accept labels with special characters", func() {
			// Act
			err := svc.UpdateLabels(ctx, "vm-1", []string{"prod-server", "tier_1", "wave.2", "env:staging"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm, err := svc.Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(vm.Labels).To(ConsistOf("prod-server", "tier_1", "wave.2", "env:staging"))
		})

	})

	Context("GetAllLabels", func() {
		// Given multiple VMs with various labels
		// When GetAllLabels is called
		// Then it should return all distinct labels
		It("should return all distinct labels across all VMs", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-b")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-2", []string{"test", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(ConsistOf("critical", "production", "staging", "test"))
			Expect(counts).To(HaveLen(4))
			// critical: vm-1, vm-2 = 2
			// production: vm-1 = 1
			// staging: vm-3 = 1
			// test: vm-2 = 1
			Expect(labels).To(Equal([]string{"critical", "production", "staging", "test"}))
			Expect(counts).To(Equal([]int{2, 1, 1, 1}))
		})

		// Given no VMs have labels
		// When GetAllLabels is called
		// Then it should return empty array
		It("should return empty array when no labels exist", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(BeEmpty())
			Expect(counts).To(BeEmpty())
		})

		// Given multiple VMs with duplicate labels
		// When GetAllLabels is called
		// Then each label should appear only once
		It("should return distinct labels (no duplicates)", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-2", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"critical", "test"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(HaveLen(3))
			Expect(labels).To(ConsistOf("critical", "production", "test"))
			Expect(counts).To(HaveLen(3))
			// critical: vm-1, vm-3 = 2
			// production: vm-1, vm-2 = 2
			// test: vm-3 = 1
			Expect(labels).To(Equal([]string{"critical", "production", "test"}))
			Expect(counts).To(Equal([]int{2, 2, 1}))
		})

		// Given labels exist
		// When GetAllLabels is called
		// Then results should be sorted alphabetically
		It("should return labels sorted alphabetically", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"zebra", "apple", "mango", "banana"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"apple", "banana", "mango", "zebra"}))
			Expect(counts).To(Equal([]int{1, 1, 1, 1}))
		})

		// Given many VMs with overlapping labels
		// When GetAllLabels is called
		// Then counts should accurately reflect usage
		It("should accurately count VMs when labels are heavily shared", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")
			insertVM("vm-4", "VM 4", "cluster-a")
			insertVM("vm-5", "VM 5", "cluster-a")
			insertVM("vm-6", "VM 6", "cluster-a")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "database", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-2", []string{"production", "database"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"production", "web"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-4", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-5", []string{"staging", "web"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-6", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"critical", "database", "production", "staging", "web"}))
			// critical: 1, database: 2, production: 4, staging: 2, web: 2
			Expect(counts).To(Equal([]int{1, 2, 4, 2, 2}))
		})

		// Given labels are dynamically updated
		// When GetAllLabels is called
		// Then counts should reflect current state
		It("should reflect updated counts after batch label operations", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			// Initial state
			err := svc.UpdateLabels(ctx, "vm-1", []string{"test"})
			Expect(err).NotTo(HaveOccurred())

			labels, counts, err := svc.GetAllLabels(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"test"}))
			Expect(counts).To(Equal([]int{1}))

			// Add test label to more VMs
			err = svc.UpdateLabels(ctx, "vm-2", []string{"test"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"test"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err = svc.GetAllLabels(ctx)

			// Assert - count should now be 3
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"test"}))
			Expect(counts).To(Equal([]int{3}))

			// Remove from one VM
			err = svc.UpdateLabels(ctx, "vm-1", []string{})
			Expect(err).NotTo(HaveOccurred())

			// Act again
			labels, counts, err = svc.GetAllLabels(ctx)

			// Assert - count should now be 2
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"test"}))
			Expect(counts).To(Equal([]int{2}))
		})

		// Given mixed label scenarios
		// When GetAllLabels is called
		// Then all counts should be accurate
		It("should handle complex mixed label scenarios", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			// vm-1: unique label
			err := svc.UpdateLabels(ctx, "vm-1", []string{"unique-to-vm1"})
			Expect(err).NotTo(HaveOccurred())

			// vm-2 and vm-3: shared label
			err = svc.UpdateLabels(ctx, "vm-2", []string{"shared-label", "another-unique"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"shared-label"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := svc.GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"another-unique", "shared-label", "unique-to-vm1"}))
			Expect(counts).To(Equal([]int{1, 2, 1}))
		})
	})

	Context("UpdateLabelVMs", func() {
		// Given multiple VMs exist
		// When UpdateLabelVMs is called to add labels
		// Then all VMs should have the label added atomically
		It("should successfully add label to all VMs", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			// Act
			err := svc.UpdateLabelVMs(ctx, []string{"vm-1", "vm-2", "vm-3"}, nil, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify all VMs have the label
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(ContainElement("production"))
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(ContainElement("production"))
			vm3, _ := svc.Get(ctx, "vm-3")
			Expect(vm3.Labels).To(ContainElement("production"))
		})

		// Given some VMs don't exist
		// When UpdateLabelVMs is called
		// Then it should fail atomically without changing any VMs
		It("should rollback all changes if any VM doesn't exist", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")

			// Act - Include non-existent vm-999
			err := svc.UpdateLabelVMs(ctx, []string{"vm-1", "vm-999", "vm-2"}, nil, "production")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))

			// Verify NO VMs got the label (transaction rolled back)
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).NotTo(ContainElement("production"))
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).NotTo(ContainElement("production"))
		})

		// Given VMs already have the label
		// When UpdateLabelVMs is called
		// Then it should be idempotent (no duplicates)
		It("should be idempotent when adding existing label", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Add "production" to both VMs (vm-1 already has it)
			err = svc.UpdateLabelVMs(ctx, []string{"vm-1", "vm-2"}, nil, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify no duplicates
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(Equal([]string{"production"})) // Still just one
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(Equal([]string{"production"}))
		})

		// Given VMs with existing labels
		// When UpdateLabelVMs is called
		// Then the new label should be appended
		It("should append to existing labels", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"critical", "wave-1"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateLabelVMs(ctx, []string{"vm-1"}, nil, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(ConsistOf("critical", "wave-1", "production"))
		})

		// Test removing labels transactionally
		It("should successfully remove label from all VMs", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-2", []string{"production", "wave-1"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = svc.UpdateLabelVMs(ctx, nil, []string{"vm-1", "vm-2", "vm-3"}, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify label removed
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(Equal([]string{"critical"}))
			Expect(vm1.Labels).NotTo(ContainElement("production"))
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(Equal([]string{"wave-1"}))
			vm3, _ := svc.Get(ctx, "vm-3")
			Expect(vm3.Labels).To(BeEmpty())
		})

		// Test rollback on remove error
		It("should rollback if any VM doesn't exist during removal", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Include non-existent vm-999
			err = svc.UpdateLabelVMs(ctx, nil, []string{"vm-1", "vm-999"}, "production")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))

			// Verify vm-1 still has the label (transaction rolled back)
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(ContainElement("production"))
		})

		// Test idempotent removal
		It("should be idempotent when removing non-existent label", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"critical"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Remove "production" which doesn't exist on these VMs
			err = svc.UpdateLabelVMs(ctx, nil, []string{"vm-1", "vm-2"}, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify vm-1 labels unchanged
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(Equal([]string{"critical"}))
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(BeEmpty())
		})

		// Test adding and removing in the same transaction
		It("should add and remove labels atomically in one transaction", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Add "production" to vm-2, remove from vm-1 and vm-3
			err = svc.UpdateLabelVMs(ctx, []string{"vm-2"}, []string{"vm-1", "vm-3"}, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(BeEmpty())
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(ContainElement("production"))
			vm3, _ := svc.Get(ctx, "vm-3")
			Expect(vm3.Labels).To(BeEmpty())
		})
	})

	Context("RemoveLabelFromAllVMs", func() {
		// Given multiple VMs have the label
		// When RemoveLabelFromAllVMs is called
		// Then it should remove the label from all VMs
		It("should remove label from all VMs that have it", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			insertVM("vm-3", "VM 3", "cluster-a")
			insertVM("vm-4", "VM 4", "cluster-a")

			err := svc.UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-2", []string{"production", "wave-1"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-3", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())
			err = svc.UpdateLabels(ctx, "vm-4", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			affected, err := svc.RemoveLabelFromAllVMs(ctx, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(3)) // vm-1, vm-2, vm-4

			// Verify label removed from all VMs
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(Equal([]string{"critical"}))
			vm2, _ := svc.Get(ctx, "vm-2")
			Expect(vm2.Labels).To(Equal([]string{"wave-1"}))
			vm3, _ := svc.Get(ctx, "vm-3")
			Expect(vm3.Labels).To(Equal([]string{"staging"})) // Unchanged
			vm4, _ := svc.Get(ctx, "vm-4")
			Expect(vm4.Labels).To(BeEmpty())
		})

		// Given no VMs have the label
		// When RemoveLabelFromAllVMs is called
		// Then it should return 0 affected
		It("should return 0 when label not in use", func() {
			// Arrange
			insertVM("vm-1", "VM 1", "cluster-a")
			insertVM("vm-2", "VM 2", "cluster-a")
			err := svc.UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			affected, err := svc.RemoveLabelFromAllVMs(ctx, "non-existent-label")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(0))

			// Verify VMs unchanged
			vm1, _ := svc.Get(ctx, "vm-1")
			Expect(vm1.Labels).To(Equal([]string{"production"}))
		})

		// Given many VMs have the label
		// When RemoveLabelFromAllVMs is called
		// Then it should return correct count
		It("should return correct count of affected VMs", func() {
			// Arrange - Create 10 VMs, all with "deprecated" label
			vmIDs := []string{"vm-01", "vm-02", "vm-03", "vm-04", "vm-05", "vm-06", "vm-07", "vm-08", "vm-09", "vm-10"}
			for _, vmID := range vmIDs {
				insertVM(vmID, "VM "+vmID, "cluster-a")
				err := svc.UpdateLabels(ctx, vmID, []string{"deprecated", "critical"})
				Expect(err).NotTo(HaveOccurred())
			}

			// Act
			affected, err := svc.RemoveLabelFromAllVMs(ctx, "deprecated")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(10))

			// Verify all VMs have label removed
			for _, vmID := range vmIDs {
				vm, _ := svc.Get(ctx, vmID)
				Expect(vm.Labels).To(Equal([]string{"critical"}))
			}
		})
	})
})
