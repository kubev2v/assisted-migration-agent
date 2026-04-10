package services

import (
	"context"
	"database/sql"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	"github.com/kubev2v/assisted-migration-agent/test"
)

// blockingStep blocks until a WaitGroup is released.
type blockingStep struct {
	block *sync.WaitGroup
}

func (b *blockingStep) Status() models.InspectionStatus {
	return models.InspectionStatus{State: models.InspectionStateRunning}
}

func (b *blockingStep) Work(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
	b.block.Wait()
	return result, nil
}

// completedStep immediately completes.
type completedStep struct{}

func (c *completedStep) Status() models.InspectionStatus {
	return models.InspectionStatus{State: models.InspectionStateCompleted}
}

func (c *completedStep) Work(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
	return result, nil
}

// noopStep immediately succeeds with running status.
type noopStep struct{}

func (n *noopStep) Status() models.InspectionStatus {
	return models.InspectionStatus{State: models.InspectionStateRunning}
}

func (n *noopStep) Work(_ context.Context, result models.InspectionResult) (models.InspectionResult, error) {
	return result, nil
}

var _ = Describe("inspectionService", func() {
	var (
		sched *scheduler.Scheduler[models.InspectionResult]
		db    *sql.DB
		st    *store.Store
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

	BeforeEach(func() {
		var err error
		sched, err = scheduler.NewScheduler[models.InspectionResult](5, 0)
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(context.Background(), db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())

		insertVM("vm-1", "test-vm-1")
		insertVM("vm-2", "test-vm-2")
	})

	AfterEach(func() {
		if sched != nil {
			sched.Close()
		}
		if db != nil {
			_ = db.Close()
		}
	})

	Describe("Cancel", func() {
		It("stops specified pipeline and persists canceled status", func() {
			var block sync.WaitGroup
			block.Add(1)
			svc := newInspectionService(st, sched, nil, nil).WithBuilder(func(id string) []InspectionWorkUnit {
				return []InspectionWorkUnit{
					&blockingStep{block: &block},
					&completedStep{},
				}
			})

			err := svc.Start([]string{"vm-1", "vm-2"}, nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.Cancel("vm-1")).To(Succeed())

			block.Done()

			Eventually(func() bool {
				return svc.IsBusy()
			}).Should(BeFalse())

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCanceled))
		})

		It("returns not found for unknown VM", func() {
			svc := newInspectionService(st, sched, nil, nil)
			err := svc.Cancel("vm-unknown")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Stop", func() {
		It("persists canceled status for all running pipelines", func() {
			var block sync.WaitGroup
			block.Add(1)
			svc := newInspectionService(st, sched, nil, nil).WithBuilder(func(id string) []InspectionWorkUnit {
				return []InspectionWorkUnit{
					&blockingStep{block: &block},
					&completedStep{},
				}
			})

			err := svc.Start([]string{"vm-1", "vm-2"}, nil)
			Expect(err).NotTo(HaveOccurred())

			svc.Stop()
			block.Done()

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCanceled))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCanceled))
		})
	})

	Describe("Start", func() {
		It("creates pipelines and persists completed status via wrapper", func() {
			svc := newInspectionService(st, sched, nil, nil).WithBuilder(func(id string) []InspectionWorkUnit {
				return []InspectionWorkUnit{
					&noopStep{},
					&completedStep{},
				}
			})

			err := svc.Start([]string{"vm-1", "vm-2"}, nil)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return svc.IsBusy()
			}).Should(BeFalse())

			Expect(getInspectionStatus("vm-1")).To(Equal(models.InspectionStateCompleted))
			Expect(getInspectionStatus("vm-2")).To(Equal(models.InspectionStateCompleted))
		})
	})
})
