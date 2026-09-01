package v2

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"

	"go.uber.org/zap"
)

const (
	// Standard Pool Configuration (High Concurrency, Lightweight)
	defaultStandardWorkers = 5
)

type InspectorService struct {
	mu              sync.Mutex
	store           *store.Store2
	inspectionLimit int
	vddkLibDir      string
	credsSvc        *CredentialsService
	cfg             *config.Agent
	buildFn         inspectionBuilderFactory
	pool            *work.Pool2[models.InspectionStatus, models.InspectionResult]
}

func NewInspectorService(st *store.Store2, inspectionLimit int, dataDir string, credsSvc *CredentialsService, cfg *config.Agent) *InspectorService {
	return &InspectorService{
		store:           st,
		inspectionLimit: inspectionLimit,
		vddkLibDir:      filepath.Join(dataDir, vddkFolder, vddkLibPath),
		credsSvc:        credsSvc,
		cfg:             cfg,
	}
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

// Start dispatches VMs to standard inspection pool.
// Pool is created on-demand per request batch to avoid re-entrant Pool2 execution limits.
func (i *InspectorService) Start(ctx context.Context, vmIds []string) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(vmIds) > i.inspectionLimit {
		return srvErrors.NewInspectionLimitReachedError(i.inspectionLimit)
	}

	if i.pool != nil && i.pool.IsRunning() {
		return fmt.Errorf("the standard inspection pool is busy processing previous inspection tasks")
	}

	creds, err := i.credsSvc.Resolve(ctx)
	if err != nil {
		return err
	}

	zap.S().Infow("starting standard inspector", "vmCount", len(vmIds))

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
		buildFn = defaultStandardInspectionBuilderFactory(i.store, vmwareOperator, detector, i.cfg)
	}

	// Mark all VMs as pending
	for _, vmID := range vmIds {
		if err = i.store.Inspection().Update(ctx, vmID, models.InspectionStatus{State: models.InspectionStatePending}); err != nil {
			return err
		}
	}

	// Provision and trigger Standard Pool
	wb := make(map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult])
	for _, vmID := range vmIds {
		wb[vmID] = buildFn(vmID)
	}
	pool := work.NewPool2(wb).WithWorkers(defaultStandardWorkers, defaultStandardWorkers).
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

func (i *InspectorService) Stop() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool == nil {
		return srvErrors.NewInspectorNotRunningError()
	}

	if err := i.pool.Stop(); err != nil {
		return err
	}
	i.pool = nil

	return nil
}

func (i *InspectorService) Cancel(virtualMachineID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool == nil || !i.pool.IsRunning() {
		return srvErrors.NewInspectorNotRunningError()
	}

	if _, err := i.pool.Cancel(virtualMachineID); err == nil {
		return nil
	}

	return srvErrors.NewResourceNotFoundError("vm", virtualMachineID)
}

func (i *InspectorService) WithInspectionBuilder(builder inspectionBuilderFactory) *InspectorService {
	i.buildFn = builder
	return i
}
