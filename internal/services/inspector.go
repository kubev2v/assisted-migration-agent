package services

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"

	"github.com/kubev2v/assisted-migration-agent/internal/store"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const defaultInspectionWorkers = 5

// InspectorService orchestrates vCenter VM inspection: one Pipeline2 per VM
// managed by a Pool2, a shared vSphere client for the run, and service-level status.
type InspectorService struct {
	mu              sync.Mutex
	pool            *work.Pool2[models.InspectionStatus, models.InspectionResult]
	buildFn         inspectionBuilderFactory
	store           *store.Store
	inspectionLimit int
	vddkLibDir      string
}

// NewInspectorService returns an idle inspector using the default inspection work units
// (validate, snapshot, inspect+save).
// inspectionLimit is the maximum distinct VMs per cycle.
func NewInspectorService(s *store.Store, inspectionLimit int, dateDir string) (*InspectorService, error) {
	return &InspectorService{
		store:           s,
		inspectionLimit: inspectionLimit,
		vddkLibDir:      filepath.Join(dateDir, vddkFolder, vddkLibPath),
	}, nil
}

func (i *InspectorService) GetStatus() models.InspectorStatus {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool != nil && i.pool.IsRunning() {
		return models.InspectorStatus{State: models.InspectorStateRunning}
	}

	return models.InspectorStatus{State: models.InspectorStateReady}
}

func (i *InspectorService) IsBusy() bool {
	return i.GetStatus().State == models.InspectorStateRunning
}

// Start connects to vSphere, starts pipelines for each vmIDs entry, and launches the pool.
func (i *InspectorService) Start(ctx context.Context, creds models.Credentials, vmIDs []string) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool != nil && i.pool.IsRunning() {
		return srvErrors.NewInspectionInProgressError()
	}

	// it's safe to nil in order to be GCed. either is already nil(from previous stop call) or not running
	i.pool = nil

	if len(vmIDs) > i.inspectionLimit {
		return srvErrors.NewInspectionLimitReachedError(i.inspectionLimit)
	}

	zap.S().Infow("starting inspector", "vmCount", len(vmIDs))

	url, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return srvErrors.NewVCenterError(err)
	}
	creds.URL = url

	if err = creds.Validate(); err != nil {
		return err
	}

	vClient, err := vmware.NewVsphereClient(ctx, &creds)
	if err != nil {
		zap.S().Named("inspector_service").Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	zap.S().Named("inspector_service").Info("vSphere connection established")

	defer func() {
		if err != nil {
			logoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			_ = vClient.Logout(logoutCtx)
		}
	}()

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

	buildFn := i.buildFn
	if buildFn == nil {
		buildFn = defaultInspectionBuilderFactory(i.store, vmwareOperator, detector)
	}

	wb := make(map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult], len(vmIDs))
	for _, id := range vmIDs {
		wb[id] = buildFn(id)
	}

	for _, id := range vmIDs {
		if err = i.store.Inspection().Update(ctx, id, models.InspectionStatus{State: models.InspectionStatePending}); err != nil {
			return err
		}
	}

	pool := work.NewPool2(wb).WithWorkers(defaultInspectionWorkers, defaultInspectionWorkers).
		WithFinalizer(func(_ context.Context) error {
			logoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			_ = vClient.Logout(logoutCtx)
			return nil
		})

	if err = pool.Start(); err != nil {
		return err
	}

	i.pool = pool

	return nil
}

func (i *InspectorService) Credentials(ctx context.Context, credentials models.Credentials) error {
	url, err := vmware.NormalizeAndValidateURL(credentials.URL)
	if err != nil {
		return srvErrors.NewVCenterError(err)
	}
	credentials.URL = url

	if err := vmware.VerifyCredentials(ctx, &credentials, "inspector"); err != nil {
		return srvErrors.NewVCenterError(err)
	}

	return nil
}

// Stop is blocking so it holds the lock while stopping the pipeline to block Start from shadowing i.pool
func (i *InspectorService) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	pool := i.pool
	i.pool = nil

	if pool == nil {
		return srvErrors.NewInspectorNotRunningError()
	}

	return pool.Stop()
}

func (i *InspectorService) Cancel(virtualMachineID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool == nil || !i.pool.IsRunning() {
		return srvErrors.NewInspectorNotRunningError()
	}

	if _, err := i.pool.Cancel(virtualMachineID); err != nil {
		return srvErrors.NewResourceNotFoundError("vm", virtualMachineID)
	}

	return nil
}

// WithInspectionBuilder replaces the default per-VM work builder factory.
func (i *InspectorService) WithInspectionBuilder(builder inspectionBuilderFactory) *InspectorService {
	i.buildFn = builder
	return i
}
