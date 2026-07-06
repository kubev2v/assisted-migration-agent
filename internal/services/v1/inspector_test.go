package v1_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/assisted-migration-agent/test"
)

// testInspectionBuilder implements work.WorkBuilder2 for inspector-level tests.
type testInspectionBuilder struct {
	units      []work.WorkUnit[models.InspectionStatus, models.InspectionResult]
	idx        int
	vmID       string
	st         *store.Store
	finalizeFn func(ctx context.Context, result models.InspectionResult) error
}

func (b *testInspectionBuilder) Next() (work.WorkUnit[models.InspectionStatus, models.InspectionResult], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[models.InspectionStatus, models.InspectionResult]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *testInspectionBuilder) Finalize(ctx context.Context, result models.InspectionResult) error {
	if b.finalizeFn != nil {
		return b.finalizeFn(ctx, result)
	}

	var status models.InspectionStatus
	switch {
	case result.Err != nil:
		status = models.InspectionStatus{State: models.InspectionStateError, Error: result.Err}
	case result.Completed:
		status = models.InspectionStatus{State: models.InspectionStateCompleted}
	default:
		status = models.InspectionStatus{State: models.InspectionStateCanceled}
	}

	if b.st != nil {
		_ = b.st.Inspection().Update(ctx, b.vmID, status)
	}

	return nil
}

// mockInspectionBuilder provides a configurable inspectionBuilderFactory for tests.
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

func (m *mockInspectionBuilder) builder() func(id string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
	return func(id string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
		running := func() models.InspectionStatus {
			return models.InspectionStatus{State: models.InspectionStateRunning}
		}

		return &testInspectionBuilder{
			vmID: id,
			st:   m.st,
			units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
				{
					Status: running,
					Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
						if m.delay > 0 {
							select {
							case <-time.After(m.delay):
							case <-ctx.Done():
								return result, ctx.Err()
							}
						}
						if err, ok := m.vmErrors[id]; ok && err != nil {
							result.Err = err
							return result, nil
						}
						m.mu.Lock()
						m.inspected = append(m.inspected, id)
						m.mu.Unlock()
						if cc := m.concerns[id]; len(cc) > 0 {
							err := m.st.WithTx(ctx, func(txCtx context.Context) error {
								return m.st.Inspection().InsertResult(txCtx, id, cc)
							})
							if err != nil {
								result.Err = err
								return result, nil
							}
						}
						result.Completed = true
						return result, nil
					},
				},
			},
		}
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
		ctx      context.Context
		db       *sql.DB
		st       *store.Store
		srv      *services.InspectorService
		credsSvc *services.CredentialsService
		tmpDir   string
	)

	mustNewInspectorService := func(s *store.Store, limit int, dir string, cSvc *services.CredentialsService) *services.InspectorService {
		svc, err := services.NewInspectorService(s, limit, dir, cSvc)
		Expect(err).NotTo(HaveOccurred())
		return svc
	}

	// Helper to query inspection status from the DB
	getInspectionStatus := func(vmID string) models.InspectionState {
		var status string
		err := db.QueryRowContext(ctx, `SELECT status FROM vm_inspection_status WHERE "VM ID" = ?`, vmID).Scan(&status)
		if err != nil {
			return models.InspectionStateNotStarted
		}
		return models.InspectionState(status)
	}

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
		tmpDir, err = os.MkdirTemp("", "inspector-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx, "")).To(Succeed())
		Expect(st.InitCollection(ctx)).To(Succeed())

		// Set up a CredentialsService with stored test credentials
		km, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		credsSvc = services.NewCredentialsService(st).WithKeyManager(km)
		creds := models.Credentials{
			URL:      "https://localhost:8989/sdk",
			Username: "user",
			Password: "pass",
			SkipTLS:  true,
		}
		err = credsSvc.Save(ctx, km.Key(), "credentials", creds)
		Expect(err).NotTo(HaveOccurred())

		// Insert test VMs into vinfo (required for foreign key constraint)
		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
		insertVM("vm-3", "test-vm-3")

		srv = mustNewInspectorService(st, 10, "", credsSvc)
	})

	AfterEach(func() {
		if srv != nil {
			_ = srv.Stop()
		}
		if db != nil {
			_ = db.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
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

	Describe("Cancel", func() {

		Context("when inspector is not started", func() {
			It("should return InspectorNotRunningError when trying to cancel VMs", func() {
				err := srv.Cancel("vm-2")
				Expect(err).To(HaveOccurred())

				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})

			It("should return nil when trying to stop idle inspector", func() {
				err := srv.Stop()
				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})
		})

		Context("when inspector is running", func() {
			BeforeEach(func() {
				// Use mock inspection service with delay to keep inspector running
				builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
				srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

				// Start inspector with all VMs (will stay running due to delay)
				err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())

				// Wait for inspector to be in running state
				Eventually(func() models.InspectorState {
					return srv.GetStatus().State
				}).Should(Equal(models.InspectorStateRunning))
			})

			It("should cancel specific pending VMs", func() {
				err := srv.Cancel("vm-2")
				Expect(err).NotTo(HaveOccurred())

				Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCanceled))
			})

			It("should cancel multiple specific VMs", func() {
				err := srv.Cancel("vm-3")
				Expect(err).NotTo(HaveOccurred())

				Expect(getInspectionStatus("vm-3")).To(Equal(models.InspectionStateCanceled))
			})
		})
	})

	Describe("Start", func() {
		It("should complete inspection successfully for single VM", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "disk", Label: "L1", Msg: "m1"},
				{Category: "net", Label: "L2", Msg: "m2"},
			})
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

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

			// Verify VM status is completed in DB
			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
		})

		It("should complete inspection successfully for multiple VMs", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			// Verify all VMs were inspected
			inspected := builder.getInspectedVMs()
			Expect(inspected).To(HaveLen(3))
			Expect(inspected).To(ContainElements("vm-1", "vm-2", "vm-3"))
		})

		It("should return VCenterError for invalid credentials", func() {
			// Store invalid credentials to trigger a VCenterError
			invalidCreds := models.Credentials{
				URL:      "https://invalid-vcenter:9999/sdk",
				Username: "bad",
				Password: "bad",
				SkipTLS:  true,
			}
			km, err := crypto.NewKeyManager("")
			Expect(err).NotTo(HaveOccurred())
			badCredsSvc := services.NewCredentialsService(st).WithKeyManager(km)
			Expect(badCredsSvc.Save(ctx, km.Key(), "credentials", invalidCreds)).To(Succeed())
			srv = mustNewInspectorService(st, 10, "", badCredsSvc)

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsVCenterError(err)).To(BeTrue())

			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateReady))
		})

		It("should mark VM as error when inspection fails and continue with next VM", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmError("vm-1", errors.New("inspection failed"))
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			// Check vm-1 status is error in DB
			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateError))

			// Check vm-2 status is completed (should continue after vm-1 error)
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
		})

		It("should preserve completed status from previous run when starting a new inspection", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			// First run
			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			err = srv.Start(ctx, []string{"vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			// vm-1 status should still be completed from the first run (persisted in DB)
			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-3")).To(Equal(models.InspectionStateCompleted))
		})

		It("should be busy while running", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(100 * time.Millisecond)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			// Should be busy while running
			Eventually(func() bool {
				return srv.IsBusy()
			}).Should(BeTrue())

			// Wait for completion
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			// Should not be busy after completion
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Stop", func() {
		It("should stop inspector and cancel all pending VMs", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			// Wait for running state
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			// Stop inspector
			err = srv.Stop()
			Expect(err).NotTo(HaveOccurred())

			// Inspector should be back to ready
			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, 10*time.Second).To(Equal(models.InspectorStateReady))

			// Should not be busy
			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Inspection limit", func() {
		It("should return InspectionLimitReachedError when Start receives more VM IDs than the limit", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 2, "", credsSvc).
				WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInspectionLimitReachedError(err)).To(BeTrue())

			var limitErr *srvErrors.InspectionLimitReachedError
			Expect(errors.As(err, &limitErr)).To(BeTrue())
			Expect(limitErr.Limit).To(Equal(2))

			Expect(srv.GetStatus().State).To(Equal(models.InspectorStateReady))
		})

		It("should allow Start when VM count equals the limit", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 2, "", credsSvc).
				WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))
		})

		It("should return InspectionLimitReachedError when Start receives more VMs than remaining limit", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
			srv = mustNewInspectorService(st, 2, "", credsSvc).
				WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInspectionLimitReachedError(err)).To(BeTrue())
		})
	})

	Describe("store persistence (mock inspection)", func() {

		It("should use only the latest inspection run for VM list concern count when the same VM is inspected twice", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "old", Label: "a", Msg: "first-run"},
			})
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			builder.withVmConcerns("vm-1", []models.VmInspectionConcern{
				{Category: "n1", Label: "b", Msg: "r2"},
				{Category: "n2", Label: "c", Msg: "r2"},
				{Category: "n3", Label: "d", Msg: "r2"},
			})

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

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
