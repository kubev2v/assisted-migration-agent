package services

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type InspectorService struct {
	scheduler *scheduler.Scheduler
	store     *store.Store

	status     models.InspectorStatus
	workToPick []models.VmWorkUnit

	mu sync.Mutex

	done chan any

	cancel context.CancelFunc
	cred   *models.Credentials
}

func NewInspectorService(s *scheduler.Scheduler, store *store.Store) *InspectorService {
	return &InspectorService{
		scheduler: s,
		status:    models.InspectorStatus{State: models.InspectorStateReady},
		store:     store,
	}
}

// GetStatus returns the current inspector status.
func (c *InspectorService) GetStatus() models.InspectorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.status
}

// GetVmStatus returns the current vm inspection status.
func (c *InspectorService) GetVmStatus(ctx context.Context, moid string) (models.InspectionStatus, error) {
	s, err := c.store.Inspection().Get(ctx, moid)
	if err != nil {
		return models.InspectionStatus{}, err
	}
	return *s, nil
}

func (c *InspectorService) Start(ctx context.Context, vmsMoid []string, cred *models.Credentials) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isBusy() {
		return srvErrors.NewInspectionInProgressError()
	}

	zap.S().Infow("starting inspector", "vmCount", len(vmsMoid))

	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan any)
	c.cred = cred

	flow := vmware.NewInspectorWorkBuilder(cred, vmsMoid).Build()

	if err := c.store.Inspection().DeleteAll(ctx); err != nil {
		return fmt.Errorf("failed to clear vms inspection table: %w", err)
	}

	for _, moid := range vmsMoid {
		if _, err := c.store.VM().Get(ctx, moid); err != nil {
			return srvErrors.NewInspectorNonExistVmError("failed to get vm '%s': %v", moid, err)
		}
	}

	if err := c.store.Inspection().UpsertMany(ctx, vmsMoid, models.InspectionStatePending); err != nil {
		return fmt.Errorf("failed to init inspection table: %w", err)
	}

	go c.run(runCtx, c.done, flow)

	return nil
}

func (c *InspectorService) run(ctx context.Context, done chan any, flow models.InspectorFlow) {
	defer close(done)
	defer func() {
		c.mu.Lock()
		if c.done == done {
			c.cancel = nil
			c.done = nil
		}
		c.mu.Unlock()
	}()

	if err := c.DoOneWorkUnit(ctx, flow.Connect); err != nil {
		zap.S().Errorw("inspector failed to connect", "error", err)
		c.setErrorStatus(err)
		return
	}

	c.setStatus(models.InspectorStatus{State: models.InspectorStateRunning})
	vmsWorkUnits := flow.Inspect

	for len(vmsWorkUnits) > 0 {
		c.mu.Lock()
		if len(c.workToPick) > 0 {
			vmsWorkUnits = append(vmsWorkUnits, c.workToPick...)
			c.workToPick = []models.VmWorkUnit{}
		}
		c.mu.Unlock()

		vmMoid := vmsWorkUnits[0].VmMoid
		unit := vmsWorkUnits[0].Work
		vmsWorkUnits = vmsWorkUnits[1:]

		s, err := c.GetVmStatus(ctx, vmMoid)
		if err != nil {
			zap.S().Errorf("failed to get vm status for vm moid %q: %v", vmMoid, err)

			c.setErrorStatus(err)

			return
		}

		if s.State != models.InspectionStatePending {
			zap.S().Debugw("skipping canceled VM inspection", "vmMoid", vmMoid)
			continue
		}

		if err := c.setVmStatus(ctx, vmMoid,
			models.InspectionStatus{State: models.InspectionStateRunning}); err != nil {
			zap.S().Errorf("failed to set vm status to running: %v", err)

			c.setErrorStatus(err)
			return
		}

		if err := c.DoOneWorkUnit(ctx, unit); err != nil {
			var e *srvErrors.InspectorWorkError
			switch {
			case errors.As(err, &e):
				zap.S().Warnw("VM inspection failed", "vmMoid", vmMoid, "error", e)

				if err := c.setVmStatus(ctx, vmMoid,
					models.InspectionStatus{State: models.InspectionStateError, Error: err}); err != nil {
					zap.S().Errorf("failed to set vm status to error: %v", err)

					c.setErrorStatus(err)
					return
				}

				continue

			default:
				c.setErrorStatus(err)
				return
			}
		}

		if err := c.setVmStatus(ctx, vmMoid,
			models.InspectionStatus{State: models.InspectionStateCompleted}); err != nil {
			zap.S().Errorf("failed to set vm status to completed: %v", err)
			c.setErrorStatus(err)

			return
		}

		zap.S().Debugw("VM inspection completed", "vmMoid", vmMoid)
	}

	c.setStatus(models.InspectorStatus{State: models.InspectorStateDone})
	zap.S().Info("inspector finished work")
}

func (c *InspectorService) AddMoreVms(ctx context.Context, vmsMoid []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	alreadyQueued, err := c.store.Inspection().List(ctx, store.NewInspectionQueryFilter().ByVmMoids(vmsMoid...))
	if err != nil {
		return fmt.Errorf("failed to list vms inspection table: %w", err)
	}

	for _, oneVmWork := range c.workToPick { // case work to pick isn't empty
		alreadyQueued[oneVmWork.VmMoid] = models.InspectionStatus{State: models.InspectionStatePending}
	}

	if len(alreadyQueued) == len(vmsMoid) {
		return fmt.Errorf("all vms already sent")
	}

	filtered := make([]string, 0, len(vmsMoid)-len(alreadyQueued))
	for _, moid := range vmsMoid {
		if _, ok := alreadyQueued[moid]; !ok {
			filtered = append(filtered, moid)
		}
	}

	if err := c.store.Inspection().UpsertMany(ctx, filtered, models.InspectionStatePending); err != nil {
		return fmt.Errorf("failed to init inspection table: %w", err)
	}

	c.workToPick = append(c.workToPick, vmware.NewInspectorWorkBuilder(c.cred, filtered).Build().Inspect...)

	return nil
}

func (c *InspectorService) DoOneWorkUnit(ctx context.Context, work models.InspectorWorkUnit) error {
	newStatus := work.Status()

	if newStatus.State != c.GetStatus().State {
		c.setStatus(newStatus)
		zap.S().Debugw("inspector changed state", "state", c.GetStatus().State)
	}

	workFn := work.Work()

	future := c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		return workFn(ctx)
	})

	select {
	case <-ctx.Done():
		future.Stop()
		c.setStatus(models.InspectorStatus{State: models.InspectorStateReady})
		return fmt.Errorf("context done")

	case result := <-future.C():
		if result.Err != nil {
			c.setStatus(models.InspectorStatus{State: models.InspectorStateError, Error: result.Err})
			return srvErrors.NewInspectorWorkError("work finished with error: %s", result.Err.Error())
		}
	}

	return nil
}

func (c *InspectorService) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		<-done
	}
}

func (c *InspectorService) setStatus(s models.InspectorStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = s
}

func (c *InspectorService) setErrorStatus(err error) {
	c.setStatus(models.InspectorStatus{
		State: models.InspectorStateError,
		Error: err,
	})
}

func (c *InspectorService) setVmStatus(ctx context.Context, vmMoid string, s models.InspectionStatus) error {
	if err := c.store.Inspection().Upsert(ctx, vmMoid, s); err != nil {
		return fmt.Errorf("upserting vm moid %s in store: %w", vmMoid, err)
	}

	return nil
}

func (c *InspectorService) CancelVmsInspection(ctx context.Context, vmsMoid []string) error {
	for _, moid := range vmsMoid {

		s, err := c.GetVmStatus(ctx, moid)
		if err != nil {
			return fmt.Errorf("failed to get vm status for moid %s: %s", moid, err)
		}

		if s.State == models.InspectionStatePending {
			if err := c.setVmStatus(ctx, moid, models.InspectionStatus{State: models.InspectionStateCanceled}); err != nil {
				return fmt.Errorf("failed to set vm status for moid %s: %s", moid, err)
			}
		}
	}

	return nil
}

func (c *InspectorService) CancelAllVmsInspection(ctx context.Context) error {
	vmsStatus, err := c.store.Inspection().List(ctx, store.NewInspectionQueryFilter().ByStatus(models.InspectionStatePending))
	if err != nil {
		return fmt.Errorf("failed to list pending vms in inspection store: %w", err)
	}

	for moid := range vmsStatus {
		if err := c.setVmStatus(ctx, moid, models.InspectionStatus{State: models.InspectionStateCanceled}); err != nil {
			return fmt.Errorf("failed to set vm status for moid %s: %s", moid, err)
		}
	}

	return nil
}

func (c *InspectorService) isBusy() bool {
	// must be protected by the caller
	switch c.status.State {
	case models.InspectorStateReady, models.InspectorStateDone, models.InspectorStateError:
		return false
	default:
		return true
	}
}

func (c *InspectorService) IsInspectorRunning() bool {
	state := c.GetStatus().State
	return state == models.InspectorStateRunning || state == models.InspectorStateConnecting
}
