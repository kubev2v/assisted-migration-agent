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
	"github.com/kubev2v/assisted-migration-agent/test"
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

// mockInspectionBuilder provides a configurable inspectionWorkBuilder for tests (per-VM inspection work units).
type mockInspectionBuilder struct {
	delay     time.Duration
	vmErrors  map[string]error
	inspected []string
	mu        sync.Mutex
	st        *store.Store
	concerns  map[string][]models.VmInspectionConcern
}

func (m *mockInspectionBuilder) withWorkDelay(d time.Duration) *mockInspectionBuilder {
	m.delay = d
	return m
}

func (m *mockInspectionBuilder) withVmError(vmID string, err error) *mockInspectionBuilder {
	m.vmErrors[vmID] = err
	return m
}

func (m *mockInspectionBuilder) withStore(st *store.Store) *mockInspectionBuilder {
	m.st = st
	return m
}

func (m *mockInspectionBuilder) withVmConcerns(vmID string, concerns []models.VmInspectionConcern) *mockInspectionBuilder {
	m.concerns[vmID] = concerns
	return m
}

func (m *mockInspectionBuilder) getInspectedVMs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.inspected...)
}

func (m *mockInspectionBuilder) builder() func(id string) models.WorkBuilder[models.InspectionStatus, models.InspectionResult] {
	return func(id string) models.WorkBuilder[models.InspectionStatus, models.InspectionResult] {
		return models.NewSliceWorkBuilder([]models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					if m.delay > 0 {
						select {
						case <-time.After(m.delay):
						case <-ctx.Done():
							return result, ctx.Err()
						}
					}
					if err, ok := m.vmErrors[id]; ok && err != nil {
						return result, err
					}
					m.mu.Lock()
					m.inspected = append(m.inspected, id)
					m.mu.Unlock()
					if m.st != nil {
						if cc := m.concerns[id]; len(cc) > 0 {
							err := m.st.WithTx(ctx, func(txCtx context.Context) error {
								return m.st.Inspection().InsertResult(txCtx, id, cc)
							})
							if err != nil {
								return result, err
							}
						}
					}
					return result, nil
				},
			},
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateCompleted}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					return result, nil
				},
			},
		})
	}
}

func newMockInspectionBuilder() *mockInspectionBuilder {
	return &mockInspectionBuilder{
		vmErrors: make(map[string]error),
		concerns: make(map[string][]models.VmInspectionConcern),
	}
}

var _ = Describe("InspectorService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		srv *services.InspectorService
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
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())

		// Insert test VMs into vinfo (required for foreign key constraint)
		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
		insertVM("vm-3", "test-vm-3")

		srv = services.NewInspectorService(st, 10, "")
	})

	AfterEach(func() {
		if srv != nil {
			_ = srv.Stop()
		}
		if db != nil {
			_ = db.Close()
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

	Describe("GetVmStatus", func() {
		It("should return NotFound state for non-existent VM when inspector never started", func() {
			status := srv.GetVmStatus("non-existent")
			Expect(status.State).To(Equal(models.InspectionStateNotStarted))
		})

		It("should return VM inspection status after start", func() {
			// Use mock inspection service with delay to keep inspector running
			builder := newMockInspectionBuilder().withWorkDelay(1 * time.Second)
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			status := srv.GetVmStatus("vm-1")
			Expect(status.State).To(Or(
				Equal(models.InspectionStatePending),
				Equal(models.InspectionStateRunning),
			))
		})
	})

	Describe("Cancel", func() {

		Context("when inspector is not started", func() {
			It("should return InspectorNotRunningError when trying to cancel VMs", func() {
				err := srv.Cancel("vm-2")
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})

			It("should return InspectorNotRunningError when trying to stop inspector", func() {
				err := srv.Stop()
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})
		})

		Context("when inspector is running", func() {
			BeforeEach(func() {
				// Use mock inspection service with delay to keep inspector running
				builder := newMockInspectionBuilder().withWorkDelay(1 * time.Second)
				srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

				err := srv.Credentials(ctx, *getVCenterCredentials())
				Expect(err).NotTo(HaveOccurred())

				// Start inspector with all VMs (will stay running due to delay)
				err = srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())

				// Wait for inspector to be in running state
				Eventually(func() models.InspectorState {
					return srv.GetStatus().State
				}).Should(Equal(models.InspectorStateRunning))
			})

			It("should cancel specific pending VMs", func() {
				err := srv.Cancel("vm-2")
				Expect(err).NotTo(HaveOccurred())

				// Check vm-2 status is canceled
				status := srv.GetVmStatus("vm-2")
				Expect(status.State).To(Equal(models.InspectionStateCanceled))

				// Other VMs should still be pending or running
				status1 := srv.GetVmStatus("vm-1")
				Expect(status1.State).To(Or(
					Equal(models.InspectionStatePending),
					Equal(models.InspectionStateRunning),
				))

				status3 := srv.GetVmStatus("vm-3")
				Expect(status3.State).To(Or(
					Equal(models.InspectionStatePending),
					Equal(models.InspectionStateRunning),
				))
			})

			It("should cancel multiple specific VMs", func() {
				err := srv.Cancel("vm-3")
				Expect(err).NotTo(HaveOccurred())

				status3 := srv.GetVmStatus("vm-3")
				Expect(status3.State).To(Equal(models.InspectionStateCanceled))

				// vm-2 should still be pending or running
				status2 := srv.GetVmStatus("vm-2")
				Expect(status2.State).To(Or(
					Equal(models.InspectionStatePending),
					Equal(models.InspectionStateRunning),
				))
			})
		})
	})

	Describe("Start", func() {
		It("should complete inspection successfully for single VM", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "disk", Label: "L1", Msg: "m1"},
				{Category: "net", Label: "L2", Msg: "m2"},
			})
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			// Verify VM was inspected
			Expect(builder.getInspectedVMs()).To(ContainElement("vm-1"))
			results, err := st.Inspection().ListResults(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].VMID).To(Equal("vm-1"))
			Expect(results[0].Concerns).To(HaveLen(2))
			Expect(results[0].Concerns).To(ContainElements(
				models.VmInspectionConcern{Category: "disk", Label: "L1", Msg: "m1"},
				models.VmInspectionConcern{Category: "net", Label: "L2", Msg: "m2"},
			))

			// Verify VM status is completed
			status := srv.GetVmStatus("vm-1")
			Expect(status.State).To(Equal(models.InspectionStateCompleted))
		})

		It("should complete inspection successfully for multiple VMs", func() {
			builder := newMockInspectionBuilder()
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			// Verify all VMs were inspected
			inspected := builder.getInspectedVMs()
			Expect(inspected).To(HaveLen(3))
			Expect(inspected).To(ContainElements("vm-1", "vm-2", "vm-3"))
		})

		It("should return error for invalid cred", func() {
			// Use invalid credentials to trigger connection error
			invalidCreds := models.Credentials{
				URL:      "https://invalid-host:8989/sdk",
				Username: "invalid",
				Password: "invalid",
			}

			err := srv.Credentials(ctx, invalidCreds)
			Expect(err).To(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(srvErrors.IsCredentialsNotSetError(err)).To(BeTrue())

			// Bad request from the user, Hence the Inspector should remain in ready state.
			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateReady))
		})

		It("should mark VM as error when inspection fails and continue with next VM", func() {
			builder := newMockInspectionBuilder().withVmError("vm-1", errors.New("inspection failed"))
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			// Check vm-1 status is error
			status1 := srv.GetVmStatus("vm-1")
			Expect(status1.State).To(Equal(models.InspectionStateError))
			Expect(status1.Error).NotTo(BeNil())

			// Check vm-2 status is completed (should continue after vm-1 error)
			status2 := srv.GetVmStatus("vm-2")
			Expect(status2.State).To(Equal(models.InspectionStateCompleted))
		})

		It("should clear previous inspection data on new start", func() {
			builder := newMockInspectionBuilder()
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			// First run
			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			err = srv.Start(ctx, []string{"vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			// Should only have vm-2 and vm-3 in pipelines; vm-1 from first run is gone
			status1 := srv.GetVmStatus("vm-1")
			Expect(status1.State).To(Equal(models.InspectionStateNotStarted))
			status2 := srv.GetVmStatus("vm-2")
			Expect(status2.State).To(Equal(models.InspectionStateCompleted))
			status3 := srv.GetVmStatus("vm-3")
			Expect(status3.State).To(Equal(models.InspectionStateCompleted))
		})

		It("should be busy while running", func() {
			builder := newMockInspectionBuilder().withWorkDelay(100 * time.Millisecond)
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			// Should be busy while running
			Eventually(func() bool {
				return srv.IsBusy()
			}).Should(BeTrue())

			// Wait for completion
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			// Should not be busy after completion
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Stop", func() {
		It("should stop inspector and cancel all pending VMs", func() {
			builder := newMockInspectionBuilder().withWorkDelay(1 * time.Second)
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			// Wait for running state
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			// Stop inspector
			err = srv.Stop()
			Expect(err).NotTo(HaveOccurred())

			// Inspector should be in canceled state
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, 10*time.Second).To(Equal(models.InspectorStateCanceled))

			// Should not be busy
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Inspection limit", func() {
		It("should return InspectionLimitReachedError when Start receives more VM IDs than the limit", func() {
			builder := newMockInspectionBuilder()
			srv = services.NewInspectorService(st, 2, "").
				WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInspectionLimitReachedError(err)).To(BeTrue())

			var limitErr *srvErrors.InspectionLimitReachedError
			Expect(errors.As(err, &limitErr)).To(BeTrue())
			Expect(limitErr.Limit).To(Equal(2))

			Expect(srv.GetStatus().State).To(Equal(models.InspectorStateReady))
		})

		It("should allow Start when VM count equals the limit", func() {
			builder := newMockInspectionBuilder()
			srv = services.NewInspectorService(st, 2, "").
				WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))
		})

		It("should return InspectionLimitReachedError when Start receives more VMs than remaining limit", func() {
			builder := newMockInspectionBuilder().withWorkDelay(1 * time.Second)
			srv = services.NewInspectorService(st, 2, "").
				WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInspectionLimitReachedError(err)).To(BeTrue())
		})
	})

	Describe("store persistence (mock inspection)", func() {

		It("should use only the latest inspection run for VM list concern count when the same VM is inspected twice", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "old", Label: "a", Msg: "first-run"},
			})
			srv = services.NewInspectorService(st, 10, "").WithInspectionBuilder(builder.builder())

			err := srv.Credentials(ctx, *getVCenterCredentials())
			Expect(err).NotTo(HaveOccurred())

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			builder.withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "n1", Label: "b", Msg: "r2"},
				{Category: "n2", Label: "c", Msg: "r2"},
				{Category: "n3", Label: "d", Msg: "r2"},
			})

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateCompleted))

			results, err := st.Inspection().ListResults(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].InspectionID).To(BeNumerically(">", results[1].InspectionID))
			Expect(results[0].Concerns).To(HaveLen(3))
			Expect(results[1].Concerns).To(HaveLen(1))

			vms, err := st.VM().List(ctx, nil, store.WithDefaultSort())
			Expect(err).NotTo(HaveOccurred())

			var vm *models.VirtualMachineSummary
			for i := range vms {
				if vms[i].ID == "vm-1" {
					vm = &vms[i]
					break
				}
			}
			Expect(vm).NotTo(BeNil())
			Expect(vm.InspectionConcernCount).To(Equal(3))
		})
	})

})
