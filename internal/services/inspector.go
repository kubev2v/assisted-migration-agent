package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vmware/govmomi"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type InspectorService struct {
	scheduler *scheduler.Scheduler[any]
	store     *store.Store
	builder   models.InspectorWorkBuilder

	status models.InspectorStatus

	mu sync.Mutex

	done chan any

	vsphereClient *govmomi.Client
	cancel        context.CancelFunc
	cred          *models.Credentials
}

// NewInspectorService creates a new InspectorService with the default vmware builder.
func NewInspectorService(s *scheduler.Scheduler[any], store *store.Store) *InspectorService {
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
func (c *InspectorService) GetVmStatus(ctx context.Context, id string) (models.InspectionStatus, error) {
	s, err := c.store.Inspection().Get(ctx, id)
	if err != nil {
		return models.InspectionStatus{}, err
	}
	return *s, nil
}

func (c *InspectorService) Start(ctx context.Context, vmIDs []string, cred *models.Credentials) error {
	if c.IsBusy() {
		return fmt.Errorf("deep inspector already in progress")
	}

	c.setState(models.InspectorStateInitiating)
	zap.S().Infow("starting inspector", "vmCount", len(vmIDs))

	vClient, err := vmware.NewVsphereClient(ctx, cred.URL, cred.Username, cred.Password, true)
	if err != nil {
		zap.S().Named("inspector_service").Errorw("failed to connect to vSphere", "error", err)
		c.setErrorStatus(err)
		return err
	}

	zap.S().Named("inspector_service").Info("vSphere connection established")

	c.vsphereClient = vClient
	c.cred = cred
	if c.builder == nil {
		c.builder = vmware.NewInspectorWorkBuilder(vmware.NewVMManager(vClient, cred.Username))
	}

	if err := c.store.Inspection().DeleteAll(ctx); err != nil {
		c.setErrorStatus(err)
		return fmt.Errorf("failed to clear vms inspection table: %w", err)
	}

	if err := c.store.Inspection().Add(ctx, vmIDs, models.InspectionStatePending); err != nil {
		c.setErrorStatus(err)
		return fmt.Errorf("failed to init inspection table: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan any)

	go c.run(runCtx, c.done)

	return nil
}

func (c *InspectorService) Add(ctx context.Context, vmIDs []string) error {
	if !c.IsBusy() {
		return srvErrors.NewInspectorNotRunningError()
	}

	if c.GetStatus().State == models.InspectorStateCanceling {
		return fmt.Errorf("inspector already canceling")
	}

	if len(vmIDs) == 0 {
		return fmt.Errorf("vmIDs is empty")
	}

	if err := c.store.Inspection().Add(ctx, vmIDs, models.InspectionStatePending); err != nil {
		return fmt.Errorf("failed to add VMs to inspection queue: %w", err)
	}

	return nil
}

func (c *InspectorService) Stop(ctx context.Context) error {
	if !c.IsBusy() {
		return srvErrors.NewInspectorNotRunningError()
	}

	c.setState(models.InspectorStateCanceling)

	// Cancel pending VMs before waiting for the goroutine to finish
	// This ensures VMs are marked as canceled even if the goroutine finishes quickly
	if err := c.CancelVmsInspection(ctx); err != nil {
		return fmt.Errorf("failed to update inspection table: %w", err)
	}

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

	c.setState(models.InspectorStateCanceled)
	zap.S().Info("inspector stopped")

	return nil
}

func (c *InspectorService) CancelVmsInspection(ctx context.Context, vmIDs ...string) error {
	if !c.IsBusy() {
		return srvErrors.NewInspectorNotRunningError()
	}

	filter := store.NewInspectionUpdateFilter().ByStatus(models.InspectionStatePending)

	if len(vmIDs) > 0 {
		filter = filter.ByVmIDs(vmIDs...)
	}

	err := c.store.Inspection().Update(ctx, filter, models.InspectionStatus{
		State: models.InspectionStateCanceled,
	})
	if err != nil {
		return fmt.Errorf("failed to update inspection table: %w", err)
	}

	return nil
}

func (c *InspectorService) IsBusy() bool {
	switch c.GetStatus().State {
	case models.InspectorStateReady, models.InspectorStateCompleted, models.InspectorStateError, models.InspectorStateCanceled:
		return false
	default:
		return true
	}
}

func (c *InspectorService) WithBuilder(builder models.InspectorWorkBuilder) *InspectorService {
	c.builder = builder
	return c
}

func (c *InspectorService) run(ctx context.Context, done chan any) {
	defer close(done)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		c.mu.Lock()
		if c.done == done {
			c.cancel = nil
			c.done = nil
		}
		c.mu.Unlock()

		c.closeVsphereClient(cleanupCtx)
	}()

	c.setState(models.InspectorStateRunning)
	zap.S().Debugw("inspector changed state", "state", c.GetStatus().State)

	for {
		id, err := c.store.Inspection().First(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break // no more pending works
			}
			zap.S().Errorw("failed to get first pending inspection", "error", err)
			c.setErrorStatus(err)
			return
		}

		if err := c.setVmState(ctx, id, models.InspectionStateRunning); err != nil {
			zap.S().Errorf("failed to set vm status to running: %v", err)
			c.setErrorStatus(err)
			return
		}

		if err := c.runVMWork(ctx, id, c.builder.Build(id)); err != nil {
			var e *srvErrors.InspectorWorkError
			switch {
			case errors.As(err, &e):
				if setError := c.setVmErrorStatus(ctx, id, err); setError != nil {
					c.setErrorStatus(err)
					return
				}
				continue // VM failed, move to next VM
			case errors.Is(err, context.Canceled):
				c.setState(models.InspectorStateCanceled)
				return
			default:
				c.setErrorStatus(err)
				return
			}
		}

		if err := c.setVmState(ctx, id, models.InspectionStateCompleted); err != nil {
			zap.S().Errorf("failed to set vm status to completed: %v", err)
			c.setErrorStatus(err)
			return
		}

		zap.S().Debugw("VM inspection completed", "vmID", id)
	}

	c.setState(models.InspectorStateCompleted)
	zap.S().Info("inspector finished work")
}

func (c *InspectorService) runVMWork(ctx context.Context, id string, units []models.InspectorWorkUnit) error {
	for _, unit := range units {

		future := c.scheduler.AddWork(func(ctx context.Context) (any, error) {
			return unit.Work()(ctx)
		})

		select {
		// Todo: handle the context done case. we may want to run some cleanup tasks
		case <-ctx.Done():
			future.Stop()
			return context.Canceled

		case result := <-future.C():
			if result.Err != nil {
				zap.S().Errorw("VM inspection failed", "vmID", id, "error", result.Err)
				return srvErrors.NewInspectorWorkError("work finished with error: %s", result.Err.Error())
			}
		}
	}
	return nil
}

func (c *InspectorService) closeVsphereClient(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vsphereClient != nil {
		_ = c.vsphereClient.Logout(ctx)
		c.vsphereClient = nil
	}
}

func (c *InspectorService) setState(s models.InspectorState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.State = s
	c.status.Error = nil
}

func (c *InspectorService) setErrorStatus(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.status = models.InspectorStatus{
		State: models.InspectorStateError,
		Error: err,
	}
}

func (c *InspectorService) setVmState(ctx context.Context, vmID string, s models.InspectionState) error {
	if err := c.store.Inspection().Update(ctx, store.NewInspectionUpdateFilter().ByVmIDs(vmID),
		models.InspectionStatus{State: s}); err != nil {
		return fmt.Errorf("updating vm %s in store: %w", vmID, err)
	}

	return nil
}

func (c *InspectorService) setVmErrorStatus(ctx context.Context, vmID string, err error) error {
	if err := c.store.Inspection().Update(ctx, store.NewInspectionUpdateFilter().ByVmIDs(vmID), models.InspectionStatus{
		State: models.InspectionStateError,
		Error: err,
	}); err != nil {
		return fmt.Errorf("updating vm %s in store: %w", vmID, err)
	}

	return nil
}
