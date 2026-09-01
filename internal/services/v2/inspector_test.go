package v2_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type testInspectionBuilder struct {
	units      []work.WorkUnit[models.InspectionStatus, models.InspectionResult]
	idx        int
	vmID       string
	st         *store.Store2
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

	status := models.TerminalStatus(result)

	if b.st != nil {
		_ = b.st.Inspection().Update(ctx, b.vmID, status)
	}

	return nil
}

type mockInspectionBuilder struct {
	delay       time.Duration
	vmErrors    map[string]error
	inspected   []string
	inFlight    int
	maxInFlight int
	mu          sync.Mutex
	st          *store.Store2
	concerns    map[string][]models.VmInspectionConcern
	// writeV2VRunning makes Work persist "running" to the v2v status table on
	// dispatch, mirroring the real unit-1 builder (which writes from Work, not
	// Status). Lets a test observe that a queued VM stays "pending" until the
	// single worker actually picks it up.
	writeV2VRunning bool
}

func (m *mockInspectionBuilder) withWorkDelay(d time.Duration) *mockInspectionBuilder {
	m.delay = d
	return m
}

func (m *mockInspectionBuilder) withVmError(vmID string, err error) *mockInspectionBuilder {
	m.vmErrors[vmID] = err
	return m
}

func (m *mockInspectionBuilder) withStore(st *store.Store2) *mockInspectionBuilder {
	m.st = st
	return m
}

func (m *mockInspectionBuilder) withV2VStatusWrites() *mockInspectionBuilder {
	m.writeV2VRunning = true
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

// getMaxInFlight returns the peak number of work units observed running
// concurrently. It equals the pool's effective worker count under load, so a
// value of 1 proves the v2v pool serialized the batch.
func (m *mockInspectionBuilder) getMaxInFlight() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxInFlight
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
						m.mu.Lock()
						m.inFlight++
						if m.inFlight > m.maxInFlight {
							m.maxInFlight = m.inFlight
						}
						m.mu.Unlock()
						defer func() {
							m.mu.Lock()
							m.inFlight--
							m.mu.Unlock()
						}()

						if m.writeV2VRunning && m.st != nil {
							_ = m.st.InspectionV2V().Update(ctx, id, models.InspectionStatus{State: models.InspectionStateRunning})
						}

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
		pool     *store.Pool
		database *store.Database
		st       *store.Store2
		mainSt   *store.Store2
		srv      *v2.InspectorService
		credsSvc *v2.CredentialsService
		tmpDir   string
	)

	mustNewInspectorService := func(s *store.Store2, limit int, dir string, cSvc *v2.CredentialsService) *v2.InspectorService {
		svc := v2.NewInspectorService(s, limit, dir, cSvc, &config.Agent{})
		return svc
	}

	getInspectionStatus := func(vmID string) models.InspectionState {
		var status string
		err := st.Querier().QueryRowContext(ctx, `SELECT status FROM vm_inspection_status WHERE "VM ID" = ?`, vmID).Scan(&status)
		if err != nil {
			return models.InspectionStateNotStarted
		}
		return models.InspectionState(status)
	}

	getV2VInspectionStatus := func(vmID string) models.InspectionState {
		var status string
		err := st.Querier().QueryRowContext(ctx, `SELECT status FROM vm_inspection_status_v2v WHERE "VM ID" = ?`, vmID).Scan(&status)
		if err != nil {
			return models.InspectionStateNotStarted
		}
		return models.InspectionState(status)
	}

	insertVM := func(id, name string) {
		_, err := st.Querier().ExecContext(ctx, `
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

		pool = store.NewPool(5 * time.Minute)

		mainPath := filepath.Join(tmpDir, "agent.duckdb")
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		mainSt, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		collPath := filepath.Join(tmpDir, "collection.duckdb")
		database, err = pool.NewDatabase("collection", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			s, err := database.Store()
			if err != nil {
				return err
			}
			parser := duckdb_parser.New(s.Querier(), nil)
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())

		km, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		credsSvc = v2.NewCredentialsService(mainSt)
		credsSvc.WithKeyManager(km)
		creds := models.Credentials{
			URL:      "https://localhost:8989/sdk",
			Username: "user",
			Password: "pass",
			SkipTLS:  true,
		}
		err = credsSvc.Save(ctx, km.Key(), "credentials", creds)
		Expect(err).NotTo(HaveOccurred())

		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
		insertVM("vm-3", "test-vm-3")

		srv = mustNewInspectorService(st, 10, "", credsSvc)
	})

	AfterEach(func() {
		if srv != nil {
			_ = srv.Stop()
		}
		pool.Close()
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
			It("should return InspectorNotRunningError", func() {
				err := srv.Cancel("vm-2")
				Expect(err).To(HaveOccurred())
				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})

			It("should return error when trying to stop idle inspector", func() {
				err := srv.Stop()
				var notRunningErr *srvErrors.InspectorNotRunningError
				Expect(errors.As(err, &notRunningErr)).To(BeTrue())
			})
		})

		Context("when inspector is running", func() {
			BeforeEach(func() {
				builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
				srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

				err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
				Expect(err).NotTo(HaveOccurred())

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

		It("persists canceled status when work unit returns a context.Canceled error", func() {
			errReturned := make(chan struct{})

			customFactory := func(id string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
				running := func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning}
				}

				return &testInspectionBuilder{
					vmID: id,
					st:   st,
					units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
						{
							Status: running,
							Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
								result.Err = fmt.Errorf("inspection interrupted: %w", context.Canceled)
								close(errReturned)
								<-ctx.Done()
								return result, nil
							},
						},
					},
				}
			}

			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(customFactory)
			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			<-errReturned
			err = srv.Cancel("vm-1")
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, 10*time.Second).Should(Equal(models.InspectorStateReady))

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCanceled))
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

			Expect(builder.getInspectedVMs()).To(ContainElement("vm-1"))
			results, err := st.Inspection().ListResults(ctx, "vm-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Concerns).To(HaveLen(2))

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
		})

		It("should complete inspection for multiple VMs", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			inspected := builder.getInspectedVMs()
			Expect(inspected).To(HaveLen(3))
			Expect(inspected).To(ContainElements("vm-1", "vm-2", "vm-3"))
		})

		It("should return VCenterError for invalid credentials", func() {
			invalidCreds := models.Credentials{
				URL:      "https://invalid-vcenter:9999/sdk",
				Username: "bad",
				Password: "bad",
				SkipTLS:  true,
			}
			km, err := crypto.NewKeyManager("")
			Expect(err).NotTo(HaveOccurred())
			badCredsSvc := v2.NewCredentialsService(mainSt)
			badCredsSvc.WithKeyManager(km)
			Expect(badCredsSvc.Save(ctx, km.Key(), "credentials", invalidCreds)).To(Succeed())
			srv = mustNewInspectorService(st, 10, "", badCredsSvc)

			err = srv.Start(ctx, []string{"vm-1"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsVCenterError(err)).To(BeTrue())

			status := srv.GetStatus()
			Expect(status.State).To(Equal(models.InspectorStateReady))
		})

		It("should mark VM as error when inspection fails and continue", func() {
			builder := newMockInspectionBuilder().withStore(st).withVmError("vm-1", errors.New("inspection failed"))
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateError))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
		})

		It("should preserve completed status from previous run when starting a new inspection", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

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

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-3")).To(Equal(models.InspectionStateCompleted))
		})

		It("should be busy while running", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(100 * time.Millisecond)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool { return srv.IsBusy() }).Should(BeTrue())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))

			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Stop", func() {
		It("should stop inspector and cancel all pending VMs", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
			srv = mustNewInspectorService(st, 10, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}).Should(Equal(models.InspectorStateRunning))

			err = srv.Stop()
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, 10*time.Second).To(Equal(models.InspectorStateReady))

			Expect(srv.IsBusy()).To(BeFalse())
		})
	})

	Describe("Inspection limit", func() {
		It("should return InspectionLimitReachedError when exceeding limit", func() {
			builder := newMockInspectionBuilder().withStore(st)
			srv = mustNewInspectorService(st, 2, "", credsSvc).WithInspectionBuilder(builder.builder())

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
			srv = mustNewInspectorService(st, 2, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return srv.GetStatus().State
			}, time.Second*10).Should(Equal(models.InspectorStateReady))
		})

		It("should return InspectionLimitReachedError when Start receives more VMs than remaining limit", func() {
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(1 * time.Second)
			srv = mustNewInspectorService(st, 2, "", credsSvc).WithInspectionBuilder(builder.builder())

			err := srv.Start(ctx, []string{"vm-1", "vm-2", "vm-3"})
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInspectionLimitReachedError(err)).To(BeTrue())
		})
	})

	Describe("store persistence", func() {
		It("should use only the latest inspection run for concern count when the same VM is inspected twice", func() {
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

	Describe("V2V pool scheduling", func() {
		// The v2v pool is throttled to a single worker (defaultV2VWorkers = 1) to
		// protect ESXi I/O. These tests dispatch two VMs at once and assert the
		// scheduler serializes them (peak concurrency 1), in contrast to the
		// multi-worker standard pool which runs them in parallel.

		It("serializes two VMs through the single-worker v2v pool", func() {
			builder := newMockInspectionBuilder().withStore(st).
				withWorkDelay(200 * time.Millisecond).withV2VStatusWrites()
			v2vSrv := v2.NewInspectorServiceV2V(st, 10, "", credsSvc, &config.Agent{}).
				WithInspectionBuilder(builder.builder())
			defer func() { _ = v2vSrv.Stop() }()

			err := v2vSrv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			// Mid-batch the pool is running exactly one VM; the other is still queued
			// and MUST read "pending", not "running". This is the user-visible contract
			// the unit-1 status fix guarantees.
			Eventually(func() []models.InspectionState {
				return []models.InspectionState{
					getV2VInspectionStatus("vm-1"), getV2VInspectionStatus("vm-2"),
				}
			}, time.Second*5, 10*time.Millisecond).Should(
				ConsistOf(models.InspectionStateRunning, models.InspectionStatePending),
			)

			// Both VMs drain and the pool returns to ready.
			Eventually(func() models.InspectorState {
				return v2vSrv.GetStatus().State
			}, time.Second*15).Should(Equal(models.InspectorStateReady))

			// Both VMs actually ran...
			Expect(builder.getInspectedVMs()).To(ConsistOf("vm-1", "vm-2"))
			// ...but never at the same time: 1 worker => peak concurrency of 1.
			Expect(builder.getMaxInFlight()).To(Equal(1))
		})

		// Regression: the pipeline calls each unit's Status() before it dispatches
		// that unit's Work, and the single v2v worker is released between units. So a
		// Status() with a DB side effect would flip a still-queued VM — or a VM parked
		// between units while another holds the worker — to "running <that stage>" in
		// the DB before the worker actually dispatches it. That produced two VMs both
		// reading "running V2V translation dry-run" when only one was truly executing.
		// Every unit must therefore persist "running" from Work(), not Status().
		It("v2v builder Status() persists nothing (only Work() writes the DB)", func() {
			Expect(st.InspectionV2V().Update(ctx, "vm-1",
				models.InspectionStatus{State: models.InspectionStatePending})).To(Succeed())

			wb := v2.DefaultV2VInspectionBuilderFactory(st, nil, nil, &config.Agent{})("vm-1")
			unitCount := 0
			for unit, more := wb.Next(); more; unit, more = wb.Next() {
				unitCount++
				// Status() is a pure in-memory progress read.
				Expect(unit.Status().State).To(Equal(models.InspectionStateRunning))
			}
			Expect(unitCount).To(BeNumerically(">", 1),
				"multi-unit pipeline is what exposes the cross-unit Status() leak")

			// Had any unit's Status() written to the store, this would read "running".
			Expect(getV2VInspectionStatus("vm-1")).To(Equal(models.InspectionStatePending))
		})

		It("standard builder Status() persists nothing (only Work() writes the DB)", func() {
			Expect(st.Inspection().Update(ctx, "vm-1",
				models.InspectionStatus{State: models.InspectionStatePending})).To(Succeed())

			wb := v2.DefaultStandardInspectionBuilderFactory(st, nil, nil, &config.Agent{})("vm-1")
			unitCount := 0
			for unit, more := wb.Next(); more; unit, more = wb.Next() {
				unitCount++
				Expect(unit.Status().State).To(Equal(models.InspectionStateRunning))
			}
			Expect(unitCount).To(BeNumerically(">", 1))

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStatePending))
		})

		It("runs the same two VMs in parallel on the multi-worker standard pool", func() {
			// Contrast case: proves the concurrency gauge detects real parallelism,
			// so the v2v result above reflects the worker cap, not a measurement
			// artifact. The standard pool has 5 workers, so two VMs overlap.
			builder := newMockInspectionBuilder().withStore(st).withWorkDelay(200 * time.Millisecond)
			stdSrv := mustNewInspectorService(st, 10, "", credsSvc).
				WithInspectionBuilder(builder.builder())
			defer func() { _ = stdSrv.Stop() }()

			err := stdSrv.Start(ctx, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectorState {
				return stdSrv.GetStatus().State
			}, time.Second*15).Should(Equal(models.InspectorStateReady))

			Expect(builder.getInspectedVMs()).To(ConsistOf("vm-1", "vm-2"))
			Expect(builder.getMaxInFlight()).To(Equal(2))
		})
	})
})
