package services

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/internal/store"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"
)

const (
	defaultInspectionSchedulerNormalWorkers   = 5
	defaultInspectionSchedulerReservedWorkers = 0
)

type (
	inspectionPipeline    = WorkPipeline[models.InspectionStatus, models.InspectionResult]
	inspectionWorkBuilder func(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult]
)

// inspectionService owns the scheduler and a map of WorkPipelines keyed by VM ID. InspectorService
// delegates Start, Stop, Cancel, and status queries here.
type inspectionService struct {
	scheduler *scheduler.Scheduler[models.InspectionResult]
	buildFn   inspectionWorkBuilder
	pipelines map[string]*inspectionPipeline
	operator  vmware.VMOperator
	mu        sync.Mutex
	detector  *vmdetect.Detector
	store     *store.Store
}

// newInspectionService returns an idle coordinator with no scheduler until Start.
func newInspectionService(s *store.Store) *inspectionService {
	return &inspectionService{
		pipelines: make(map[string]*inspectionPipeline),
		store:     s,
	}
}

// Start creates the scheduler, resets the pipeline map, and starts one pipeline per vmID.
func (i *inspectionService) Start(operator *vmware.VMManager, detector *vmdetect.Detector, vmIDs []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.operator = operator

	sched, err := scheduler.NewScheduler[models.InspectionResult](defaultInspectionSchedulerNormalWorkers, defaultInspectionSchedulerReservedWorkers)
	if err != nil {
		return err
	}
	i.scheduler = sched

	if i.buildFn == nil {
		i.buildFn = i.buildInspectionWorkUnits
	}

	i.pipelines = make(map[string]*inspectionPipeline)

	i.detector = detector

	zap.S().Named("inspection_service").Infow("starting VM inspection pipelines", "vmCount", len(vmIDs), "vmIds", vmIDs)

	for _, id := range vmIDs {
		pipeline := NewWorkPipeline(models.InspectionStatus{State: models.InspectionStatePending}, i.scheduler, i.buildFn(id))
		_ = pipeline.Start()
		i.pipelines[id] = pipeline
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

// WithWorkUnitsBuilder sets the function that produces work units per VM.
func (i *inspectionService) WithWorkUnitsBuilder(builder inspectionWorkBuilder) *inspectionService {
	i.buildFn = builder
	return i
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
				snapId, err := i.createSnapshot(ctx, id)
				result.SnapshotID = snapId
				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				var (
					concerns []models.VmInspectionConcern
					err      error
				)

				defer func() {
					err = errors.Join(err, i.removeSnapshot(ctx, id, result.SnapshotID))
				}()

				concerns, err = i.inspect(ctx, id, result.SnapshotID)
				result.Concerns = concerns

				return result, err
			},
		},
		{
			Status: func() models.InspectionStatus {
				return models.InspectionStatus{State: models.InspectionStateRunning}
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				err := i.save(ctx, id, result.Concerns)
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
	zap.S().Named("inspection_service").Infow("validating VM privileges for inspection", "vmId", id)
	if err := i.operator.ValidatePrivileges(ctx, id, models.RequiredPrivileges); err != nil {
		zap.S().Named("inspection_service").Errorw("privilege validation failed", "vmId", id, "error", err)
		return err
	}
	zap.S().Named("inspection_service").Infow("privilege validation passed", "vmId", id)
	return nil
}

func (i *inspectionService) createSnapshot(ctx context.Context, id string) (string, error) {
	zap.S().Named("inspection_service").Infow("creating VM snapshot", "vmId", id)
	req := vmware.CreateSnapshotRequest{
		VmId:         id,
		SnapshotName: models.InspectionSnapshotName,
		Description:  "",
		Memory:       false,
		Quiesce:      false,
	}

	snapID, err := i.operator.CreateSnapshot(ctx, req)
	if err != nil {
		zap.S().Named("inspection_service").Errorw("failed to create VM snapshot", "vmId", id, "error", err)
		return "", err
	}

	zap.S().Named("inspection_service").Infow("VM snapshot created", "vmId", id)

	return snapID, nil
}

func (i *inspectionService) inspect(ctx context.Context, vmId, snapId string) ([]models.VmInspectionConcern, error) {
	zap.S().Named("inspection_service").Infow("running deep inspection", "vmId", vmId, "snapshotId", snapId)

	result, err := i.detector.Detect(vmdetect.DetectParams{
		Ctx:           ctx,
		VMMoref:       vmId,
		SnapshotMoref: snapId,
	})

	if err != nil {
		zap.S().Named("inspection_service").Errorw("deep inspection failed", "vmId", vmId, "snapshotId", snapId, "error", err)
		return nil, err
	}

	zap.S().Named("inspection_service").Infow("inspection completed", "vmId", vmId, "snapshotId", snapId)

	var out []models.VmInspectionConcern

	if result.AllConcerns != nil {
		out = make([]models.VmInspectionConcern, 0, len(result.AllConcerns))
		for _, c := range result.AllConcerns {
			out = append(out, models.VmInspectionConcern{
				Label:    c.Label,
				Category: string(c.Category),
				Msg:      c.Message,
			})
		}
	}

	zap.S().Named("inspection_service").Infow("deep inspection completed", "vmId", vmId, "concernCount", len(out))

	return out, nil
}

func (i *inspectionService) save(ctx context.Context, id string, concerns []models.VmInspectionConcern) error {
	zap.S().Named("inspection_service").Infow("persisting inspection results", "vmId", id, "concernCount", len(concerns))
	err := i.store.WithTx(ctx, func(txCtx context.Context) error {
		return i.store.Inspection().InsertResult(txCtx, id, concerns)
	})
	if err != nil {
		zap.S().Named("inspection_service").Errorw("failed to persist inspection results", "vmId", id, "error", err)
		return err
	}
	zap.S().Named("inspection_service").Infow("inspection results persisted", "vmId", id)
	return nil
}

func (i *inspectionService) removeSnapshot(ctx context.Context, vmId, snapId string) error {

	zap.S().Named("inspection_service").Infow("removing VM snapshot", "vmId", vmId)

	removeSnapReq := vmware.RemoveSnapshotRequest{
		SnapshotId:  snapId,
		Consolidate: true,
	}

	if err := i.operator.RemoveSnapshot(ctx, removeSnapReq); err != nil {
		zap.S().Named("inspection_service").Errorw("failed to remove VM snapshot", "vmId", vmId, "error", err)
		return err
	}

	zap.S().Named("inspection_service").Infow("VM snapshot removed", "vmId", vmId)

	return nil
}
