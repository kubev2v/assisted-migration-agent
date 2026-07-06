package store_test

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMStore Labels", func() {
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

	// Helper to insert VM into vinfo table
	insertVM := func(id, name, cluster string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template")
			VALUES (?, ?, 'poweredOn', ?, 4096, false)
		`, id, name, cluster)
		Expect(err).NotTo(HaveOccurred())
	}

	Context("labels field in List()", func() {
		// Given a VM exists with labels
		// When List is called
		// Then it should return VM with labels array
		It("should return labels for VM with labels set", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production", "critical"}))
		})

		// Given a VM exists with single label
		// When List is called
		// Then it should return VM with single label in array
		It("should return single label correctly", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-2", []string{"test"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-2"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"test"}))
		})

		// Given a VM exists but labels were never set
		// When List is called
		// Then it should return empty array from DEFAULT '[]'
		It("should return empty array for VM without labels", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")

			// Act
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-3"})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(BeEmpty()) // Default from schema
		})

		// Given a VM exists with labels
		// When labels are updated
		// Then the value should be stored in vinfo table
		It("should store labels directly in vinfo table", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"label1", "label2"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Query vinfo directly
			var labelsJSON string
			err = db.QueryRowContext(ctx,
				`SELECT "labels" FROM vinfo WHERE "VM ID" = ?`,
				"vm-1").Scan(&labelsJSON)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			// DuckDB stores JSON arrays in its own format
			Expect(labelsJSON).To(MatchRegexp(`\[.*label1.*label2.*\]`))
		})
	})

	Context("UpdateLabels", func() {
		// Given a VM exists in the database
		// When UpdateLabels is called with multiple labels
		// Then the VM should have those labels
		It("should successfully set multiple labels", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")

			// Act
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical", "wave-1"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify via List
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production", "critical", "wave-1"}))
		})

		// Given a VM exists and has labels
		// When UpdateLabels is called with empty array
		// Then the VM should have no labels
		It("should successfully clear labels with empty array", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-2", []string{"old-label"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-2"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(BeEmpty())
		})

		// Given a VM exists and has labels
		// When UpdateLabels is called with nil
		// Then the VM should have no labels (treated as empty array)
		It("should handle nil labels as empty array", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-3", []string{"old-label"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().UpdateLabels(ctx, "vm-3", nil)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-3"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(BeEmpty())
		})

		// Given a VM exists
		// When UpdateLabels is called with labels containing single quotes
		// Then they should be properly escaped and stored
		It("should handle labels with single quotes (SQL escaping)", func() {
			// Arrange
			insertVM("vm-4", "Test VM 4", "cluster-a")

			// Act
			err := s.VM().UpdateLabels(ctx, "vm-4", []string{"prod's-server", "O'Reilly"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-4"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"prod's-server", "O'Reilly"}))
		})

		// Given a VM exists and has labels
		// When UpdateLabels is called again
		// Then labels should be replaced (not appended)
		It("should replace labels (not append)", func() {
			// Arrange
			insertVM("vm-5", "Test VM 5", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-5", []string{"label1", "label2"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().UpdateLabels(ctx, "vm-5", []string{"label3"})

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-5"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"label3"}))
		})

		// Given no VM exists with the given ID
		// When UpdateLabels is called
		// Then it should return an error
		It("should return error for non-existent VM", func() {
			// Act
			err := s.VM().UpdateLabels(ctx, "non-existent-vm", []string{"label"})

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("GetAllLabels", func() {
		// Given multiple VMs with various labels
		// When GetAllLabels is called
		// Then it should return distinct labels sorted
		It("should return all distinct labels across all VMs", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			insertVM("vm-3", "Test VM 3", "cluster-a")

			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{"test", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-3", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(ConsistOf("critical", "production", "staging", "test"))
			Expect(counts).To(HaveLen(4))
			// critical: vm-1, vm-2 = 2
			// production: vm-1 = 1
			// staging: vm-3 = 1
			// test: vm-2 = 1
			// Labels are sorted alphabetically, so counts should match that order
			Expect(labels).To(Equal([]string{"critical", "production", "staging", "test"}))
			Expect(counts).To(Equal([]int{2, 1, 1, 1}))
		})

		// Given VMs exist but none have labels
		// When GetAllLabels is called
		// Then it should return empty array
		It("should return empty array when no labels exist", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(BeEmpty())
			Expect(counts).To(BeEmpty())
		})

		// Given multiple VMs with duplicate labels
		// When GetAllLabels is called
		// Then it should return each label only once
		It("should return distinct labels (no duplicates)", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			insertVM("vm-3", "Test VM 3", "cluster-a")

			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{"production", "test"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-3", []string{"critical"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(HaveLen(3))
			Expect(labels).To(ConsistOf("critical", "production", "test"))
			Expect(counts).To(HaveLen(3))
			// critical: vm-1, vm-3 = 2
			// production: vm-1, vm-2 = 2
			// test: vm-2 = 1
			Expect(labels).To(Equal([]string{"critical", "production", "test"}))
			Expect(counts).To(Equal([]int{2, 2, 1}))
		})

		// Given labels exist
		// When GetAllLabels is called
		// Then results should be sorted alphabetically
		It("should return labels sorted alphabetically", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"zebra", "apple", "mango", "banana"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"apple", "banana", "mango", "zebra"}))
			Expect(counts).To(Equal([]int{1, 1, 1, 1}))
		})

		// Given VMs with many shared labels
		// When GetAllLabels is called
		// Then counts should accurately reflect VM count per label
		It("should return accurate counts when multiple VMs share labels", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			insertVM("vm-3", "Test VM 3", "cluster-a")
			insertVM("vm-4", "Test VM 4", "cluster-a")
			insertVM("vm-5", "Test VM 5", "cluster-a")

			// All VMs have production
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical", "database"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-3", []string{"production", "web"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-4", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-5", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			// Verify expected labels
			Expect(labels).To(Equal([]string{"critical", "database", "production", "staging", "web"}))
			// Verify counts:
			// critical: vm-1, vm-2 = 2
			// database: vm-1 = 1
			// production: vm-1, vm-2, vm-3, vm-4 = 4
			// staging: vm-5 = 1
			// web: vm-3 = 1
			Expect(counts).To(Equal([]int{2, 1, 4, 1, 1}))
		})

		// Given a single VM with multiple labels
		// When GetAllLabels is called
		// Then each label should have count of 1
		It("should return count of 1 for each label when single VM has multiple labels", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"label-a", "label-b", "label-c", "label-d"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"label-a", "label-b", "label-c", "label-d"}))
			Expect(counts).To(Equal([]int{1, 1, 1, 1}))
		})

		// Given labels are added and then removed
		// When GetAllLabels is called
		// Then counts should reflect current state
		It("should return accurate counts after labels are removed from VMs", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			insertVM("vm-3", "Test VM 3", "cluster-a")

			// Add labels
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{"production"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-3", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Verify initial count
			labels, counts, err := s.VM().GetAllLabels(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"production"}))
			Expect(counts).To(Equal([]int{3}))

			// Remove label from one VM
			err = s.VM().UpdateLabels(ctx, "vm-1", []string{})
			Expect(err).NotTo(HaveOccurred())

			// Act
			labels, counts, err = s.VM().GetAllLabels(ctx)

			// Assert - count should now be 2
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"production"}))
			Expect(counts).To(Equal([]int{2}))
		})

		// Given label with exact count boundary
		// When GetAllLabels is called
		// Then counts should be exact not approximate
		It("should return exact counts not approximate", func() {
			// Arrange - create 10 VMs with same label
			for i := 1; i <= 10; i++ {
				vmID := fmt.Sprintf("vm-%d", i)
				insertVM(vmID, fmt.Sprintf("Test VM %d", i), "cluster-a")
				err := s.VM().UpdateLabels(ctx, vmID, []string{"shared-label"})
				Expect(err).NotTo(HaveOccurred())
			}

			// Act
			labels, counts, err := s.VM().GetAllLabels(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(labels).To(Equal([]string{"shared-label"}))
			Expect(counts).To(Equal([]int{10}))
		})
	})

	Context("AddLabel", func() {
		// Given a VM exists with no labels
		// When AddLabel is called
		// Then the label should be added to the VM
		It("should add label to VM with no existing labels", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")

			// Act
			err := s.VM().AddLabel(ctx, "vm-1", "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production"}))
		})

		// Given a VM exists with existing labels
		// When AddLabel is called
		// Then the new label should be appended
		It("should add label to VM with existing labels (append)", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-2", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().AddLabel(ctx, "vm-2", "wave-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-2"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(ConsistOf("production", "critical", "wave-1"))
		})

		// Given a VM exists with a label
		// When AddLabel is called with the same label
		// Then no duplicate should be created (idempotent)
		It("should be idempotent (no duplicates when adding existing label)", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-3", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Add the same label again
			err = s.VM().AddLabel(ctx, "vm-3", "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-3"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production"})) // Still only one
		})

		// Given no VM exists with the given ID
		// When AddLabel is called
		// Then it should return ResourceNotFoundError
		It("should return error for non-existent VM", func() {
			// Act
			err := s.VM().AddLabel(ctx, "non-existent-vm", "production")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		// Given a VM exists
		// When AddLabel is called with label containing special characters
		// Then it should be stored correctly
		It("should handle labels with special characters", func() {
			// Arrange
			insertVM("vm-4", "Test VM 4", "cluster-a")

			// Act
			err := s.VM().AddLabel(ctx, "vm-4", "prod's-server")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-4"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"prod's-server"}))
		})
	})

	Context("RemoveLabel", func() {
		// Given a VM exists with multiple labels
		// When RemoveLabel is called
		// Then only the specified label should be removed
		It("should remove label from VM with multiple labels", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical", "wave-1"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().RemoveLabel(ctx, "vm-1", "critical")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(ConsistOf("production", "wave-1"))
			Expect(vms[0].Labels).NotTo(ContainElement("critical"))
		})

		// Given a VM exists with single label
		// When RemoveLabel is called
		// Then the VM should have empty labels array
		It("should remove label from VM with single label (leaves empty array)", func() {
			// Arrange
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-2", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.VM().RemoveLabel(ctx, "vm-2", "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-2"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(BeEmpty())
		})

		// Given a VM exists without the label
		// When RemoveLabel is called
		// Then it should be a no-op (idempotent)
		It("should be idempotent (removing non-existent label is no-op)", func() {
			// Arrange
			insertVM("vm-3", "Test VM 3", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-3", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act - Remove a label that doesn't exist
			err = s.VM().RemoveLabel(ctx, "vm-3", "staging")

			// Assert - Should succeed (no error)
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-3"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(Equal([]string{"production"})) // Unchanged
		})

		// Given no VM exists with the given ID
		// When RemoveLabel is called
		// Then it should return ResourceNotFoundError
		It("should return error for non-existent VM", func() {
			// Act
			err := s.VM().RemoveLabel(ctx, "non-existent-vm", "production")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		// Given a VM exists with no labels
		// When RemoveLabel is called
		// Then it should be a no-op
		It("should handle removing from VM with no labels", func() {
			// Arrange
			insertVM("vm-4", "Test VM 4", "cluster-a")

			// Act
			err := s.VM().RemoveLabel(ctx, "vm-4", "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-4"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Labels).To(BeEmpty())
		})
	})

	Context("RemoveLabelGlobally", func() {
		// Given multiple VMs have the same label
		// When RemoveLabelGlobally is called
		// Then the label should be removed from all VMs
		It("should remove label from all VMs that have it", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			insertVM("vm-3", "Test VM 3", "cluster-a")

			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production", "critical"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-2", []string{"production", "wave-1"})
			Expect(err).NotTo(HaveOccurred())
			err = s.VM().UpdateLabels(ctx, "vm-3", []string{"staging"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			affected, err := s.VM().RemoveLabelGlobally(ctx, "production")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(2)) // vm-1 and vm-2

			// Verify vm-1 no longer has "production"
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms[0].Labels).To(Equal([]string{"critical"}))

			// Verify vm-2 no longer has "production"
			vms, err = s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-2"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms[0].Labels).To(Equal([]string{"wave-1"}))

			// Verify vm-3 unchanged
			vms, err = s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-3"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms[0].Labels).To(Equal([]string{"staging"}))
		})

		// Given no VMs have the specified label
		// When RemoveLabelGlobally is called
		// Then it should return 0 affected
		It("should return 0 when no VMs have the label", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			insertVM("vm-2", "Test VM 2", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"production"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			affected, err := s.VM().RemoveLabelGlobally(ctx, "non-existent-label")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(0))

			// Verify vm-1 unchanged
			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms[0].Labels).To(Equal([]string{"production"}))
		})

		// Given a VM has the label as its only label
		// When RemoveLabelGlobally is called
		// Then the VM should have empty labels array
		It("should leave empty array when removing last label", func() {
			// Arrange
			insertVM("vm-1", "Test VM 1", "cluster-a")
			err := s.VM().UpdateLabels(ctx, "vm-1", []string{"deprecated"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			affected, err := s.VM().RemoveLabelGlobally(ctx, "deprecated")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(1))

			vms, err := s.VM().List(ctx, sq.Eq{`v."VM ID"`: "vm-1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(vms[0].Labels).To(BeEmpty())
		})

		// Given many VMs have the same label
		// When RemoveLabelGlobally is called
		// Then it should return correct count
		It("should return correct count of affected VMs", func() {
			// Arrange
			vmIDs := []string{"vm-1", "vm-2", "vm-3", "vm-4", "vm-5"}
			for _, vmID := range vmIDs {
				insertVM(vmID, "Test VM "+vmID, "cluster-a")
				err := s.VM().UpdateLabels(ctx, vmID, []string{"batch-label"})
				Expect(err).NotTo(HaveOccurred())
			}

			// Act
			affected, err := s.VM().RemoveLabelGlobally(ctx, "batch-label")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(affected).To(Equal(5))
		})
	})
})
