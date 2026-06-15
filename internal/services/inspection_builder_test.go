package services

import (
	"context"
	"database/sql"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("inspectionBuilder", func() {
	var (
		db *sql.DB
		st *store.Store
	)

	insertVM := func(id, name string) {
		_, err := db.ExecContext(context.Background(), `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096)
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	getInspectionStatus := func(vmID string) models.InspectionState {
		var status string
		err := db.QueryRowContext(context.Background(), `SELECT status FROM vm_inspection_status WHERE "VM ID" = ?`, vmID).Scan(&status)
		if err != nil {
			return models.InspectionStateNotStarted
		}
		return models.InspectionState(status)
	}

	getInspectionDetails := func(vmID string) string {
		var details sql.NullString
		err := db.QueryRowContext(context.Background(), `SELECT details FROM vm_inspection_status WHERE "VM ID" = ?`, vmID).Scan(&details)
		if err != nil {
			return ""
		}
		return details.String
	}

	newBuilder := func(id string, workFn func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error)) *inspectionBuilder {
		running := func() models.InspectionStatus {
			return models.InspectionStatus{State: models.InspectionStateRunning}
		}
		return &inspectionBuilder{
			vmID:  id,
			store: st,
			units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
				{Status: running, Work: workFn},
			},
		}
	}

	BeforeEach(func() {
		var err error

		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(context.Background(), db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())

		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Describe("Status persistence", func() {
		It("should persist status with details to the DB from the Status callback", func() {
			gate := make(chan struct{})

			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": &inspectionBuilder{
					vmID:  "vm-1",
					store: st,
					units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
						{
							Status: func() models.InspectionStatus {
								status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating credentials"}
								_ = st.Inspection().Update(context.Background(), "vm-1", status)
								return status
							},
							Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
								select {
								case <-gate:
									result.Completed = true
									return result, nil
								case <-ctx.Done():
									return result, ctx.Err()
								}
							},
						},
					},
				},
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() string {
				return getInspectionDetails("vm-1")
			}).Should(Equal("validating credentials"))

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateRunning))

			close(gate)
			Eventually(pool.IsRunning).Should(BeFalse())
			_ = pool.Stop()
		})

		It("should update details as the pipeline progresses through steps", func() {
			gate1 := make(chan struct{})
			gate2 := make(chan struct{})

			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": &inspectionBuilder{
					vmID:  "vm-1",
					store: st,
					units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
						{
							Status: func() models.InspectionStatus {
								status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "step one"}
								_ = st.Inspection().Update(context.Background(), "vm-1", status)
								return status
							},
							Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
								select {
								case <-gate1:
									return result, nil
								case <-ctx.Done():
									return result, ctx.Err()
								}
							},
						},
						{
							Status: func() models.InspectionStatus {
								status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "step two"}
								_ = st.Inspection().Update(context.Background(), "vm-1", status)
								return status
							},
							Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
								select {
								case <-gate2:
									result.Completed = true
									return result, nil
								case <-ctx.Done():
									return result, ctx.Err()
								}
							},
						},
					},
				},
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() string {
				return getInspectionDetails("vm-1")
			}).Should(Equal("step one"))

			close(gate1)

			Eventually(func() string {
				return getInspectionDetails("vm-1")
			}).Should(Equal("step two"))

			close(gate2)
			Eventually(pool.IsRunning).Should(BeFalse())
			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
		})

		It("should set terminal details from Finalize", func() {
			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": &inspectionBuilder{
					vmID:  "vm-1",
					store: st,
					units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
						{
							Status: func() models.InspectionStatus {
								status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "working"}
								_ = st.Inspection().Update(context.Background(), "vm-1", status)
								return status
							},
							Work: func(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
								result.Completed = true
								return result, nil
							},
						},
					},
				},
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())
			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionDetails("vm-1")).To(Equal("completed"))
		})
	})

	Describe("Cancel", func() {
		It("persists canceled status when pipeline is canceled", func() {
			gate := make(chan struct{})

			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": newBuilder("vm-1", func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					select {
					case <-gate:
						result.Completed = true
						return result, nil
					case <-ctx.Done():
						return result, ctx.Err()
					}
				}),
				"vm-2": newBuilder("vm-2", func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					select {
					case <-gate:
						result.Completed = true
						return result, nil
					case <-ctx.Done():
						return result, ctx.Err()
					}
				}),
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			_, _ = pool.Cancel("vm-1")

			close(gate)

			Eventually(func() bool {
				return !pool.IsRunning()
			}).Should(BeTrue())
			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCanceled))
		})
	})

	Describe("Stop", func() {
		It("persists canceled status for all running pipelines", func() {
			gate := make(chan struct{})

			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": newBuilder("vm-1", func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					select {
					case <-gate:
						result.Completed = true
						return result, nil
					case <-ctx.Done():
						return result, ctx.Err()
					}
				}),
				"vm-2": newBuilder("vm-2", func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					select {
					case <-gate:
						result.Completed = true
						return result, nil
					case <-ctx.Done():
						return result, ctx.Err()
					}
				}),
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCanceled))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCanceled))
		})
	})

	Describe("Error", func() {
		It("persists error status when work unit sets result.Err", func() {
			domainErr := errors.New("privilege check failed")

			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": newBuilder("vm-1", func(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					result.Err = domainErr
					return result, nil
				}),
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() bool {
				return !pool.IsRunning()
			}).Should(BeTrue())
			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateError))
		})
	})

	Describe("Completion", func() {
		It("persists completed status via Finalize", func() {
			wb := map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]{
				"vm-1": newBuilder("vm-1", func(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					result.Completed = true
					return result, nil
				}),
				"vm-2": newBuilder("vm-2", func(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					result.Completed = true
					return result, nil
				}),
			}

			pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, 0)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() bool {
				return !pool.IsRunning()
			}).Should(BeTrue())
			_ = pool.Stop()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
		})
	})
})
