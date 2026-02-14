package services_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

// getVCenterCredentials returns test credentials for vCenter.
// vcsim accepts any username/password, but we use standard test values.
func getVCenterCredentials() *models.Credentials {
	return &models.Credentials{
		URL:      "https://localhost:8989/sdk",
		Username: "user",
		Password: "pass",
	}
}

// testsMockInspectorWorkBuilder implements models.InspectorWorkBuilder for testing
type testsMockInspectorWorkBuilder struct {
	vmWorkErr map[string]error // per-VM errors
	workDelay time.Duration
	inspected []string
	mu        sync.Mutex
}

func newMockInspectorWorkBuilder() *testsMockInspectorWorkBuilder {
	return &testsMockInspectorWorkBuilder{
		vmWorkErr: make(map[string]error),
		inspected: make([]string, 0),
	}
}

func (m *testsMockInspectorWorkBuilder) withVmError(vmID string, err error) *testsMockInspectorWorkBuilder {
	m.vmWorkErr[vmID] = err
	return m
}

func (m *testsMockInspectorWorkBuilder) withWorkDelay(d time.Duration) *testsMockInspectorWorkBuilder {
	m.workDelay = d
	return m
}

func (m *testsMockInspectorWorkBuilder) getInspectedVMs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.inspected))
	copy(result, m.inspected)
	return result
}

func (m *testsMockInspectorWorkBuilder) Build(vmID string) []models.InspectorWorkUnit {
	return []models.InspectorWorkUnit{
		{
			Work: func() func(ctx context.Context) (any, error) {
				return func(ctx context.Context) (any, error) {
					if m.workDelay > 0 {
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-time.After(m.workDelay):
						}
					}

					if err, ok := m.vmWorkErr[vmID]; ok && err != nil {
						return nil, err
					}

					m.mu.Lock()
					m.inspected = append(m.inspected, vmID)
					m.mu.Unlock()

					return nil, nil
				}
			},
		},
	}
}

var _ = Describe("InspectorService", func() {
	var (
		ctx   context.Context
		db    *sql.DB
		st    *store.Store
		sched *scheduler.Scheduler
		srv   *services.InspectorService
	)

	// Helper to insert test VMs into vinfo table
	insertVM := func(id, name string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096)
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		// Insert test VMs into vinfo (required for foreign key constraint)
		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
		insertVM("vm-3", "test-vm-3")

		st = store.NewStore(db)
		sched = scheduler.NewScheduler(1)
		srv = services.NewInspectorService(sched, st)
	})

	AfterEach(func() {
		if srv != nil {
			_ = srv.Stop(ctx)
		}
		if sched != nil {
			sched.Close()
		}
		if db != nil {
			db.Close()
		}
	})

	Describe("GetStatus", func() {
		It("should return ready state initially", func() {
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateReady))
		})
	})

	Describe("IsBusy", func() {
		It("should return false when in ready state", func() {
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Add VMs to inspection queue", func() {

		Context("when inspector is not started", func() {
			It("should return InspectorNotRunningError when trying to add VMs", func() {
				err := srv.Add(ctx, []string{"vm-1", "vm-2"})
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})
		})

		Context("when inspector is running", func() {
			BeforeEach(func() {
				// Insert an initial VM for starting the inspector
				insertVM("vm-0", "test-vm-0")

				// Use a mock builder with delay to keep inspector running
				builder := newMockInspectorWorkBuilder().withWorkDelay(1 * time.Second)
				srv = services.NewInspectorService(sched, st).WithBuilder(builder)

				// Start inspector with vm-0 (will stay running due to delay)
				err := srv.Start(ctx, []string{"vm-0"}, getVCenterCredentials())
				Expect(err).NotTo(HaveOccurred())

				// Wait for inspector to be in running state
				Eventually(func() models.InspectorState {
					return srv.GetStatus().State
				}).Should(Equal(models.InspectorStateRunning))
			})

			It("should add VMs to inspection table with pending status", func() {
				err := srv.Add(ctx, []string{"vm-1", "vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())

				// Verify added VMs are in DB with pending status
				for _, vmID := range []string{"vm-1", "vm-2", "vm-3"} {
					status, err := st.Inspection().Get(ctx, vmID)
					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(models.InspectionStatePending))
				}
			})

			It("should not duplicate VMs when adding same VM twice", func() {
				err := srv.Add(ctx, []string{"vm-1", "vm-2"})
				Expect(err).NotTo(HaveOccurred())

				err = srv.Add(ctx, []string{"vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())

				// Should have vm-0 (from Start) + vm-1, vm-2, vm-3 (from Add) = 4 total
				statuses, err := st.Inspection().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(statuses).To(HaveLen(4))
			})

			It("should return error for empty VM list", func() {
				// Get current count before adding empty list
				before, err := st.Inspection().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				err = srv.Add(ctx, []string{})
				Expect(err).To(HaveOccurred())

				// Count should not change
				after, err := st.Inspection().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(after)).To(Equal(len(before)))
			})
		})
	})

	Describe("GetVmStatus", func() {
		It("should return error for non-existent VM", func() {
			_, err := srv.GetVmStatus(ctx, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should return VM inspection status after adding", func() {
			// Insert an initial VM and start inspector
			insertVM("vm-0", "test-vm-0")

			// Use a mock builder with delay to keep inspector running
			builder := newMockInspectorWorkBuilder().withWorkDelay(1 * time.Second)
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-0"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			err = srv.Add(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			status, err := srv.GetVmStatus(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(models.InspectionStatePending))
		})
	})

	Describe("CancelVmsInspection", func() {

		Context("when inspector is not started", func() {
			It("should return InspectorNotRunningError when trying to cancel VMs", func() {
				err := srv.CancelVmsInspection(ctx, "vm-1", "vm-2")
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})

			It("should return InspectorNotRunningError when trying to cancel all VMs", func() {
				err := srv.CancelVmsInspection(ctx)
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})
		})

		Context("when inspector is running", func() {
			BeforeEach(func() {
				// Insert an initial VM for starting the inspector
				insertVM("vm-0", "test-vm-0")

				// Use a mock builder with delay to keep inspector running
				builder := newMockInspectorWorkBuilder().withWorkDelay(1 * time.Second)
				srv = services.NewInspectorService(sched, st).WithBuilder(builder)

				// Start inspector with vm-0 (will stay running due to delay)
				err := srv.Start(ctx, []string{"vm-0"}, getVCenterCredentials())
				Expect(err).NotTo(HaveOccurred())

				// Wait for inspector to be in running state
				Eventually(func() models.InspectorState {
					return srv.GetStatus().State
				}).Should(Equal(models.InspectorStateRunning))

				// Add VMs to the inspection queue
				err = srv.Add(ctx, []string{"vm-1", "vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should cancel specific pending VMs", func() {
				err := srv.CancelVmsInspection(ctx, "vm-2")
				Expect(err).NotTo(HaveOccurred())

				// Check vm-2 status is canceled
				status, err := st.Inspection().Get(ctx, "vm-2")
				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(models.InspectionStateCanceled))

				// Other VMs should still be pending
				status1, err := st.Inspection().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(status1.State).To(Equal(models.InspectionStatePending))

				status3, err := st.Inspection().Get(ctx, "vm-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(status3.State).To(Equal(models.InspectionStatePending))
			})

			It("should cancel multiple specific VMs", func() {
				err := srv.CancelVmsInspection(ctx, "vm-1", "vm-3")
				Expect(err).NotTo(HaveOccurred())

				// Check vm-1 and vm-3 are canceled
				status1, err := st.Inspection().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(status1.State).To(Equal(models.InspectionStateCanceled))

				status3, err := st.Inspection().Get(ctx, "vm-3")
				Expect(err).NotTo(HaveOccurred())
				Expect(status3.State).To(Equal(models.InspectionStateCanceled))

				// vm-2 should still be pending
				status2, err := st.Inspection().Get(ctx, "vm-2")
				Expect(err).NotTo(HaveOccurred())
				Expect(status2.State).To(Equal(models.InspectionStatePending))
			})

			It("should cancel all pending VMs when no specific IDs provided", func() {
				err := srv.CancelVmsInspection(ctx)
				Expect(err).NotTo(HaveOccurred())

				// vm-1, vm-2, vm-3 should be canceled (vm-0 is running, not pending)
				statuses, err := st.Inspection().List(ctx, store.NewInspectionQueryFilter().ByStatus(models.InspectionStateCanceled))
				Expect(err).NotTo(HaveOccurred())
				Expect(statuses).To(HaveLen(3))
			})

			It("should not cancel already completed VMs", func() {
				// Mark vm-1 as completed
				err := st.Inspection().Update(ctx,
					store.NewInspectionUpdateFilter().ByVmIDs("vm-1"),
					models.InspectionStatus{State: models.InspectionStateCompleted})
				Expect(err).NotTo(HaveOccurred())

				// Cancel all pending
				err = srv.CancelVmsInspection(ctx)
				Expect(err).NotTo(HaveOccurred())

				// vm-1 should still be completed (not canceled)
				status1, err := st.Inspection().Get(ctx, "vm-1")
				Expect(err).NotTo(HaveOccurred())
				Expect(status1.State).To(Equal(models.InspectionStateCompleted))

				// vm-2 and vm-3 should be canceled
				status2, err := st.Inspection().Get(ctx, "vm-2")
				Expect(err).NotTo(HaveOccurred())
				Expect(status2.State).To(Equal(models.InspectionStateCanceled))
			})
		})
	})

	Describe("Start", func() {
		var (
			builder *testsMockInspectorWorkBuilder
		)

		It("should complete inspection successfully for single VM", func() {
			builder = newMockInspectorWorkBuilder()
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-1"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// Verify VM was inspected
			Expect(builder.getInspectedVMs()).To(ContainElement("vm-1"))

			// Verify VM status is completed in DB
			status, err := st.Inspection().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(models.InspectionStateCompleted))
		})

		It("should complete inspection successfully for multiple VMs", func() {
			builder = newMockInspectorWorkBuilder()
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// Verify all VMs were inspected
			inspected := builder.getInspectedVMs()
			Expect(inspected).To(HaveLen(3))
			Expect(inspected).To(ContainElements("vm-1", "vm-2", "vm-3"))
		})

		It("should process VMs in sequence order", func() {
			builder = newMockInspectorWorkBuilder()
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// VMs should be processed in order they were added
			Expect(builder.getInspectedVMs()).To(Equal([]string{"vm-1", "vm-2", "vm-3"}))
		})

		It("should return error for invalid cred", func() {
			// Use invalid credentials to trigger connection error
			// The builder's init error won't be triggered since connection happens first
			invalidCreds := &models.Credentials{
				URL:      "https://invalid-host:8989/sdk",
				Username: "invalid",
				Password: "invalid",
			}

			err := srv.Start(ctx, []string{"vm-1"}, invalidCreds)
			Expect(err).To(HaveOccurred())
			// The error could be "connection refused", "no such host", "timeout", etc.
			// Just check that it's a connection-related error
			errMsg := err.Error()
			Expect(errMsg).To(Or(
				ContainSubstring("connection refused"),
				ContainSubstring("no such host"),
				ContainSubstring("timeout"),
				ContainSubstring("connection"),
				ContainSubstring("failed to connect"),
				ContainSubstring("dial tcp"),
			))

			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateError))
			Expect(status.Error).NotTo(BeNil())
		})

		It("should mark VM as error when inspection fails and continue with next VM", func() {
			builder = newMockInspectorWorkBuilder().withVmError("vm-1", errors.New("inspection failed"))
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-1", "vm-2"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// Check vm-1 status is error
			status1, err := st.Inspection().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status1.State).To(Equal(models.InspectionStateError))
			Expect(status1.Error).NotTo(BeNil())

			// Check vm-2 status is completed (should continue after vm-1 error)
			status2, err := st.Inspection().Get(ctx, "vm-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(status2.State).To(Equal(models.InspectionStateCompleted))
		})

		It("should clear previous inspection data on new start", func() {
			builder = newMockInspectorWorkBuilder()
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			// First run
			err := srv.Start(ctx, []string{"vm-1"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			err = srv.Start(ctx, []string{"vm-2", "vm-3"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// Should only have vm-2 and vm-3, not vm-1
			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(2))
			Expect(statuses).To(HaveKey("vm-2"))
			Expect(statuses).To(HaveKey("vm-3"))
			Expect(statuses).NotTo(HaveKey("vm-1"))
		})

		It("should be busy while running", func() {
			builder = newMockInspectorWorkBuilder().withWorkDelay(100 * time.Millisecond)
			srv = services.NewInspectorService(sched, st).WithBuilder(builder)

			err := srv.Start(ctx, []string{"vm-1"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			// Should be busy while running
			Eventually(func() bool {
				return srv.IsBusy()
			}).Should(BeTrue())

			// Wait for completion
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateCompleted))

			// Should not be busy after completion
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("CancelInspector", func() {
		It("should stop inspector and cancel all pending VMs", func() {
			srv = services.NewInspectorService(sched, st)

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"}, getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			// Wait for running state
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			// Cancel inspector
			err = srv.Stop(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Inspector should be in canceled state
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateCanceled))

			// Should not be busy
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

})

var _ = Describe("InspectionStore", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
	)

	// Helper to insert test VMs into vinfo table
	insertVM := func(id, name string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096)
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		// Insert test VMs into vinfo (required for foreign key constraint)
		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
		insertVM("vm-3", "test-vm-3")
		insertVM("vm-a", "test-vm-a")
		insertVM("vm-b", "test-vm-b")
		insertVM("vm-c", "test-vm-c")

		st = store.NewStore(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Describe("Add", func() {
		It("should add VMs with pending status", func() {
			err := st.Inspection().Add(ctx, []string{"vm-1", "vm-2"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(2))
			Expect(statuses["vm-1"].State).To(Equal(models.InspectionStatePending))
			Expect(statuses["vm-2"].State).To(Equal(models.InspectionStatePending))
		})

		It("should ignore duplicates on conflict", func() {
			err := st.Inspection().Add(ctx, []string{"vm-1"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			// Try to add same VM again
			err = st.Inspection().Add(ctx, []string{"vm-1"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(1))
		})

		It("should handle empty list", func() {
			err := st.Inspection().Add(ctx, []string{}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Get", func() {
		BeforeEach(func() {
			err := st.Inspection().Add(ctx, []string{"vm-1"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return inspection status for existing VM", func() {
			status, err := st.Inspection().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(models.InspectionStatePending))
		})

		It("should return error for non-existent VM", func() {
			_, err := st.Inspection().Get(ctx, "non-existent")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			err := st.Inspection().Add(ctx, []string{"vm-1", "vm-2", "vm-3"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			// Mark vm-2 as completed
			err = st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-2"),
				models.InspectionStatus{State: models.InspectionStateCompleted})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should list all inspections without filter", func() {
			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(3))
		})

		It("should filter by status", func() {
			statuses, err := st.Inspection().List(ctx,
				store.NewInspectionQueryFilter().ByStatus(models.InspectionStatePending))
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(2))
			Expect(statuses).To(HaveKey("vm-1"))
			Expect(statuses).To(HaveKey("vm-3"))
		})

		It("should filter by VM IDs", func() {
			statuses, err := st.Inspection().List(ctx,
				store.NewInspectionQueryFilter().ByVmIDs("vm-1", "vm-2"))
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(2))
			Expect(statuses).To(HaveKey("vm-1"))
			Expect(statuses).To(HaveKey("vm-2"))
		})

		It("should apply limit", func() {
			statuses, err := st.Inspection().List(ctx,
				store.NewInspectionQueryFilter().Limit(2))
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(2))
		})
	})

	Describe("First", func() {
		It("should return first pending VM by sequence", func() {
			// Add VMs in order
			err := st.Inspection().Add(ctx, []string{"vm-1"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
			err = st.Inspection().Add(ctx, []string{"vm-2"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
			err = st.Inspection().Add(ctx, []string{"vm-3"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			first, err := st.Inspection().First(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(Equal("vm-1"))
		})

		It("should skip non-pending VMs", func() {
			err := st.Inspection().Add(ctx, []string{"vm-1", "vm-2", "vm-3"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())

			// Mark vm-1 as completed
			err = st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-1"),
				models.InspectionStatus{State: models.InspectionStateCompleted})
			Expect(err).NotTo(HaveOccurred())

			first, err := st.Inspection().First(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(Equal("vm-2"))
		})

		It("should return error when no pending VMs", func() {
			_, err := st.Inspection().First(ctx)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, sql.ErrNoRows)).To(BeTrue())
		})
	})

	Describe("Update", func() {
		BeforeEach(func() {
			err := st.Inspection().Add(ctx, []string{"vm-1", "vm-2", "vm-3"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update status for specific VM", func() {
			err := st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-1"),
				models.InspectionStatus{State: models.InspectionStateRunning})
			Expect(err).NotTo(HaveOccurred())

			status, err := st.Inspection().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(models.InspectionStateRunning))
		})

		It("should update status with error", func() {
			testErr := errors.New("inspection failed")
			err := st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-1"),
				models.InspectionStatus{State: models.InspectionStateError, Error: testErr})
			Expect(err).NotTo(HaveOccurred())

			status, err := st.Inspection().Get(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(models.InspectionStateError))
			Expect(status.Error).NotTo(BeNil())
			Expect(status.Error.Error()).To(Equal("inspection failed"))
		})

		It("should update multiple VMs by status", func() {
			err := st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByStatus(models.InspectionStatePending),
				models.InspectionStatus{State: models.InspectionStateCanceled})
			Expect(err).NotTo(HaveOccurred())

			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			for _, s := range statuses {
				Expect(s.State).To(Equal(models.InspectionStateCanceled))
			}
		})
	})

	Describe("DeleteAll", func() {
		BeforeEach(func() {
			err := st.Inspection().Add(ctx, []string{"vm-1", "vm-2", "vm-3"}, models.InspectionStatePending)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should delete all inspections", func() {
			err := st.Inspection().DeleteAll(ctx)
			Expect(err).NotTo(HaveOccurred())

			statuses, err := st.Inspection().List(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(statuses).To(HaveLen(0))
		})
	})

	Describe("Processing order", func() {
		It("should maintain insertion order via sequence", func() {
			// Add VMs one by one
			for _, id := range []string{"vm-c", "vm-a", "vm-b"} {
				err := st.Inspection().Add(ctx, []string{id}, models.InspectionStatePending)
				Expect(err).NotTo(HaveOccurred())
			}

			// First should return in insertion order
			first, err := st.Inspection().First(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(Equal("vm-c"))

			// Mark as completed and get next
			err = st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-c"),
				models.InspectionStatus{State: models.InspectionStateCompleted})
			Expect(err).NotTo(HaveOccurred())

			second, err := st.Inspection().First(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(second).To(Equal("vm-a"))

			// Mark as completed and get next
			err = st.Inspection().Update(ctx,
				store.NewInspectionUpdateFilter().ByVmIDs("vm-a"),
				models.InspectionStatus{State: models.InspectionStateCompleted})
			Expect(err).NotTo(HaveOccurred())

			third, err := st.Inspection().First(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(third).To(Equal("vm-b"))
		})
	})
})
