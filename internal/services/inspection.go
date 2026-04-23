package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/store"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"
)

type inspectionPipeline = WorkPipeline[models.InspectionStatus, models.InspectionResult]

// InspectionWorkUnit is the interface for a single inspection step.
// inspectionService wraps these into models.WorkUnit with status persistence.
type InspectionWorkUnit interface {
	Status() models.InspectionStatus
	Work(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error)
}

type inspectionWorkBuilder = func(id string) []InspectionWorkUnit

// inspectionService is a one time consumable, enforced by the consumed channel.
// Second Start() call is a no-op
// InspectorService delegates Start, Stop, Cancel, and status queries here.
type inspectionService struct {
	scheduler     *scheduler.Scheduler[models.InspectionResult]
	buildFn       inspectionWorkBuilder
	pipelines     map[string]*inspectionPipeline
	operator      vmware.VMOperator
	mu            sync.Mutex
	detector      *vmdetect.Detector
	store         *store.Store
	stop          chan struct{}
	waitCleanupCh chan struct{}
	consumed      chan struct{}
	cleanUpFn     func()
}

func newInspectionService(s *store.Store, scheduler *scheduler.Scheduler[models.InspectionResult], operator *vmware.VMManager, detector *vmdetect.Detector) *inspectionService {
	return &inspectionService{
		pipelines: make(map[string]*inspectionPipeline),
		scheduler: scheduler,
		store:     s,
		operator:  operator,
		detector:  detector,
		consumed:  make(chan struct{}, 1),
	}
}

func (i *inspectionService) Start(vmIDs []string, cleanupFn func()) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.buildFn == nil {
		i.buildFn = i.buildInspectionWorkUnits
	}

	select {
	case i.consumed <- struct{}{}:
	default:
		return nil
	}

	i.stop = make(chan struct{}, 1)
	i.pipelines = make(map[string]*inspectionPipeline)
	if cleanupFn != nil {
		i.cleanUpFn = cleanupFn
	}

	zap.S().Named("inspection_service").Infow("starting VM inspection pipelines", "vmCount", len(vmIDs), "vmIds", vmIDs)

	for _, id := range vmIDs {
		i.updateStatus(id, models.InspectionStatus{State: models.InspectionStatePending})

		units := i.wrapWorkUnits(id, i.buildFn(id))
		pipeline := NewWorkPipeline(models.InspectionStatus{State: models.InspectionStatePending}, i.scheduler, units)
		if err := pipeline.Start(); err != nil {
			zap.S().Named("inspection_service").Errorw("failed to start VM inspection pipeline", "vmId", id, "error", err)
			continue
		}
		i.pipelines[id] = pipeline
	}

	go i.run()

	return nil
}

func (i *inspectionService) Stop() {
	i.mu.Lock()
	if i.stop == nil {
		i.mu.Unlock()
		return
	}

	pipelines := i.pipelines
	i.waitCleanupCh = make(chan struct{})
	stopCh := i.stop
	i.mu.Unlock()

	for id, pipeline := range pipelines {
		if pipeline != nil && pipeline.IsRunning() {
			pipeline.Stop()
			i.updateStatus(id, models.InspectionStatus{State: models.InspectionStateCanceled})
		}
	}

	// if we can fill up the channel here, it means run() is still running.
	// and we're safe to wait for waitCleanupCh
	select {
	case stopCh <- struct{}{}:
		<-i.waitCleanupCh // wait for run to clean up
	default:
	}

	i.stop = nil
}

// WithBuilder sets the function that produces work units per VM.
func (i *inspectionService) WithBuilder(builder inspectionWorkBuilder) *inspectionService {
	i.buildFn = builder
	return i
}

// Cancel stops the pipeline for id, if present.
func (i *inspectionService) Cancel(id string) error {
	i.mu.Lock()
	pipelines := i.pipelines
	i.mu.Unlock()

	p, ok := pipelines[id]
	if !ok {
		return srvErrors.NewResourceNotFoundError("vm", id)
	}

	p.Stop()
	i.updateStatus(id, models.InspectionStatus{State: models.InspectionStateCanceled})

	return nil
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

// wrapWorkUnits wraps []InspectionWorkUnit into []models.WorkUnit with status persistence.
func (i *inspectionService) wrapWorkUnits(id string, units []InspectionWorkUnit) []models.WorkUnit[models.InspectionStatus, models.InspectionResult] {
	wrapped := make([]models.WorkUnit[models.InspectionStatus, models.InspectionResult], len(units))
	for idx, u := range units {
		u := u
		wrapped[idx] = models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
			Status: func() models.InspectionStatus {
				s := u.Status()
				i.updateStatus(id, s)
				return s
			},
			Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				r, err := u.Work(ctx, result)
				if err != nil {
					i.updateStatus(id, models.InspectionStatus{State: models.InspectionStateError, Error: err})
				}
				return r, err
			},
		}
	}
	return wrapped
}

func (i *inspectionService) run() {
	ticker := time.NewTicker(5 * time.Second)

	defer func() {
		ticker.Stop()
		if i.cleanUpFn != nil {
			i.cleanUpFn()
		}
		if i.waitCleanupCh != nil {
			close(i.waitCleanupCh)
		}
	}()

	for {
		select {
		case <-i.stop:
			return
		case <-ticker.C:
			if !i.IsBusy() {

				// try to fill up the channel to be sure
				// Stop() will not wait on waitCleanupCh while run exit normally
				select {
				case i.stop <- struct{}{}:
				default:
				}
				return
			}
		}
	}
}

// inspectionStep is a concrete InspectionWorkUnit.
type inspectionStep struct {
	status models.InspectionStatus
	work   func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error)
}

func (s *inspectionStep) Status() models.InspectionStatus { return s.status }
func (s *inspectionStep) Work(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
	return s.work(ctx, result)
}

// buildInspectionWorkUnits is the default pipeline: validate privileges, snapshot, inspect, save, remove snapshot.
func (i *inspectionService) buildInspectionWorkUnits(id string) []InspectionWorkUnit {
	return []InspectionWorkUnit{
		&inspectionStep{
			status: models.InspectionStatus{State: models.InspectionStateRunning},
			work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				return result, i.validate(ctx, id)
			},
		},
		&inspectionStep{
			status: models.InspectionStatus{State: models.InspectionStateRunning},
			work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				snapId, err := i.createSnapshot(ctx, id)
				result.SnapshotID = snapId
				return result, err
			},
		},
		&inspectionStep{
			status: models.InspectionStatus{State: models.InspectionStateRunning},
			work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
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
		&inspectionStep{
			status: models.InspectionStatus{State: models.InspectionStateRunning},
			work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
				return result, i.save(ctx, id, result.Concerns)
			},
		},
		&inspectionStep{
			status: models.InspectionStatus{State: models.InspectionStateCompleted},
			work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
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

func (i *inspectionService) updateStatus(id string, status models.InspectionStatus) {
	if err := i.store.Inspection().Update(context.Background(), id, status); err != nil {
		zap.S().Named("inspection_service").Errorw("failed to persist inspection status", "vmId", id, "state", status.State, "error", err)
	}
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
