package services

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"

	"github.com/kubev2v/assisted-migration-agent/internal/store"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	defaultInspectionWorkers         = 5
	defaultPriorityInspectionWorkers = 0
)

// InspectorService orchestrates vCenter VM inspection: one asynchronous WorkPipeline per VM,
// a shared vSphere client for the run, and service-level status.
type InspectorService struct {
	mu              sync.Mutex
	inspectionSvc   *inspectionService
	buildFn         inspectionWorkBuilder
	store           *store.Store
	inspectionLimit int
	scheduler       *scheduler.Scheduler[models.InspectionResult]
	vddkLibDir      string
}

// NewInspectorService returns an idle inspector using the default inspection work units
// (validate, snapshot, inspect, save, remove snapshot).
// inspectionLimit is the maximum distinct VMs per cycle (Start batch + Add)
func NewInspectorService(s *store.Store, inspectionLimit int, dateDir string) (*InspectorService, error) {
	scheduler, err := scheduler.NewScheduler[models.InspectionResult](defaultInspectionWorkers, defaultPriorityInspectionWorkers)
	if err != nil {
		return nil, err
	}
	return &InspectorService{
		store:           s,
		inspectionLimit: inspectionLimit,
		vddkLibDir:      filepath.Join(dateDir, vddkFolder, vddkLibPath),
		scheduler:       scheduler,
	}, nil
}

// GetStatus returns the current inspector status.
func (i *InspectorService) GetStatus() models.InspectorStatus {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.inspectionSvc == nil {
		return models.InspectorStatus{State: models.InspectorStateReady}
	}

	return models.InspectorStatus{State: models.InspectorStateRunning}
}

// IsBusy reports whether the inspector is currently running.
func (i *InspectorService) IsBusy() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.inspectionSvc != nil
}

// Start connects to vSphere, starts pipelines for each vmIDs entry, and launches the run loop.
func (i *InspectorService) Start(ctx context.Context, creds models.Credentials, vmIDs []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.inspectionSvc != nil && i.inspectionSvc.IsBusy() {
		return srvErrors.NewInspectionInProgressError()
	}

	if len(vmIDs) > i.inspectionLimit {
		return srvErrors.NewInspectionLimitReachedError(i.inspectionLimit)
	}

	zap.S().Infow("starting inspector", "vmCount", len(vmIDs))

	vClient, err := vmware.NewVsphereClient(ctx, creds.URL, creds.Username, creds.Password, true)
	if err != nil {
		zap.S().Named("inspector_service").Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	zap.S().Named("inspector_service").Info("vSphere connection established")

	detector, err := vmdetect.NewDetector(vmdetect.DetectorConfig{
		Credentials: vmdetect.Credentials{
			VCenterURL: creds.URL,
			Username:   creds.Username,
			Password:   creds.Password,
		},
		VDDKLibDir: i.vddkLibDir,
		Logger:     logrus.StandardLogger(),
	})
	if err != nil {
		return err
	}

	vmwareOperator := vmware.NewVMManager(vClient, creds.Username)
	i.inspectionSvc = newInspectionService(i.store, i.scheduler, vmwareOperator, detector)
	if i.buildFn != nil {
		i.inspectionSvc.WithBuilder(i.buildFn)
	}
	if err := i.inspectionSvc.Start(vmIDs, func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		_ = vClient.Logout(logoutCtx)

		i.mu.Lock()
		i.inspectionSvc = nil
		i.mu.Unlock()
	}); err != nil {
		logoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		_ = vClient.Logout(logoutCtx)
		i.inspectionSvc = nil
		return err
	}

	return nil
}

// Credentials verifies vCenter credentials without storing them.
func (i *InspectorService) Credentials(ctx context.Context, credentials models.Credentials) error {
	if err := vmware.VerifyCredentials(ctx, &credentials, "inspector"); err != nil {
		return srvErrors.NewVCenterError(err)
	}

	return nil
}

// Stop requests cancellation of all VM pipelines and tears down the scheduler.
func (i *InspectorService) Stop() error {
	i.mu.Lock()

	if i.inspectionSvc == nil {
		i.mu.Unlock()
		return nil
	}

	currentInspectionSrv := i.inspectionSvc
	i.inspectionSvc = nil

	i.mu.Unlock()

	currentInspectionSrv.Stop()

	return nil
}

// Cancel stops the pipeline for a single VM ID.
func (i *InspectorService) Cancel(id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.inspectionSvc == nil {
		return srvErrors.NewInspectorNotRunningError()
	}

	return i.inspectionSvc.Cancel(id)
}

// WithInspectionBuilder replaces the default per-VM work unit list.
func (i *InspectorService) WithInspectionBuilder(builder inspectionWorkBuilder) *InspectorService {
	i.buildFn = builder
	return i
}
