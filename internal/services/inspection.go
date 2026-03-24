package services

import (
	"context"
	"errors"
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type (
	inspectionPipeline    = WorkPipeline[models.InspectionStatus, models.InspectionResult]
	inspectionWorkBuilder func(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult]
)

// inspectionService owns the scheduler and a map of WorkPipelines keyed by VM ID. InspectorService
// delegates Start, Add, Stop, Cancel, and status queries here.
type inspectionService struct {
	scheduler *scheduler.Scheduler[models.InspectionResult]
	buildFn   inspectionWorkBuilder
	pipelines map[string]*inspectionPipeline
	operator  vmware.VMOperator
	mu        sync.Mutex
}

// newInspectionService returns an idle coordinator with no scheduler until Start.
func newInspectionService() *inspectionService {
	return &inspectionService{
		pipelines: make(map[string]*inspectionPipeline),
	}
}

// WithWorkUnitsBuilder sets the function that produces work units per VM.
func (i *inspectionService) WithWorkUnitsBuilder(builder inspectionWorkBuilder) *inspectionService {
	i.buildFn = builder
	return i
}

// Add starts a new pipeline for id unless one is already running for that id.
func (i *inspectionService) Add(id string) error {
	i.mu.Lock()
	pipeline, found := i.pipelines[id]
	i.mu.Unlock()

	if found && pipeline != nil && pipeline.IsRunning() {
		return srvErrors.NewInspectionInProgressError()
	}

	p := NewWorkPipeline(models.InspectionStatus{State: models.InspectionStatePending}, i.scheduler, i.buildFn(id))
	_ = p.Start()

	i.mu.Lock()
	i.pipelines[id] = p
	i.mu.Unlock()

	return nil
}

// TotalPipelines returns the number of VM pipelines registered in the current inspection cycle.
func (i *inspectionService) TotalPipelines() int {
	i.mu.Lock()
	defer i.mu.Unlock()

	return len(i.pipelines)
}

// CancelVmInspection stops the pipeline for id, if present.
func (i *inspectionService) CancelVmInspection(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if p, ok := i.pipelines[id]; ok {
		p.Stop()
	}
}

// IsBusy reports whether any registered pipeline is still running.
func (i *inspectionService) IsBusy() bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	for _, p := range i.pipelines {
		if p.IsRunning() {
			return true
		}
	}

	return false
}

// GetVmStatus returns pull-based status from the VM’s pipeline (completed, running, error, canceled).
func (i *inspectionService) GetVmStatus(id string) models.InspectionStatus {
	i.mu.Lock()
	pipeline, found := i.pipelines[id]
	i.mu.Unlock()

	if !found {
		return models.InspectionStatus{State: models.InspectionStateNotStarted}
	}

	state := pipeline.State()
	if state.Err != nil {
		if errors.Is(state.Err, errPipelineStopped) {
			return models.InspectionStatus{State: models.InspectionStateCanceled, Error: state.Err}
		}
		return models.InspectionStatus{State: models.InspectionStateError, Error: state.Err}
	}

	return state.State
}

// Start creates the scheduler, resets the pipeline map, and starts one pipeline per vmID.
func (i *inspectionService) Start(operator *vmware.VMManager, vmIDs []string) error {
	i.operator = operator

	sched := scheduler.NewScheduler[models.InspectionResult](5)
	i.scheduler = sched

	if i.buildFn == nil {
		i.buildFn = i.buildInspectionWorkUnits
	}

	i.mu.Lock()
	i.pipelines = make(map[string]*inspectionPipeline)
	i.mu.Unlock()

	for _, id := range vmIDs {
		pipeline := NewWorkPipeline(models.InspectionStatus{State: models.InspectionStatePending}, i.scheduler, i.buildFn(id))
		_ = pipeline.Start()
		i.mu.Lock()
		i.pipelines[id] = pipeline
		i.mu.Unlock()
	}

	return nil
}

// Stop stops every pipeline under lock, then closes the scheduler.
func (i *inspectionService) Stop() {
	i.mu.Lock()
	for _, pipeline := range i.pipelines {
		p := pipeline
		if p != nil {
			p.Stop()
		}
	}
	i.mu.Unlock()

	s := i.scheduler
	i.scheduler = nil
	if s != nil {
		s.Close()
	}
}

// buildInspectionWorkUnits is the default pipeline: validate privileges, snapshot, inspect, save, remove snapshot.
func (i *inspectionService) buildInspectionWorkUnits(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult] {
	return []models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.validate(ctx, id)
				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.createSnapshot(ctx, id)
				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.inspect(id)
				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.save(ctx, id)
				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.removeSnapshot(ctx, id)
				return result, err
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
	}
}

func (i *inspectionService) validate(ctx context.Context, id string) error {
	return i.operator.ValidatePrivileges(ctx, id, models.RequiredPrivileges)
}

func (i *inspectionService) createSnapshot(ctx context.Context, id string) error {
	zap.S().Named("inspector_service").Infow("creating VM snapshot", "vmId", id)
	req := vmware.CreateSnapshotRequest{
		VmId:         id,
		SnapshotName: models.InspectionSnapshotName,
		Description:  "",
		Memory:       false,
		Quiesce:      false,
	}

	if err := i.operator.CreateSnapshot(ctx, req); err != nil {
		zap.S().Named("inspector_service").Errorw("failed to create VM snapshot", "vmId", id, "error", err)
		return err
	}

	zap.S().Named("inspector_service").Infow("VM snapshot created", "vmId", id)

	return nil
}

func (i *inspectionService) inspect(id string) error {
	return nil
}

func (i *inspectionService) save(ctx context.Context, id string) error {
	return nil
}

func (i *inspectionService) removeSnapshot(ctx context.Context, id string) error {

	zap.S().Named("inspector_service").Infow("removing VM snapshot", "vmId", id)

	removeSnapReq := vmware.RemoveSnapshotRequest{
		VmId:         id,
		SnapshotName: models.InspectionSnapshotName,
		Consolidate:  true,
	}

	if err := i.operator.RemoveSnapshot(ctx, removeSnapReq); err != nil {
		zap.S().Named("inspector_service").Errorw("failed to remove VM snapshot", "vmId", id, "error", err)
		return err
	}

	zap.S().Named("inspector_service").Infow("VM snapshot removed", "vmId", id)

	return nil
}
