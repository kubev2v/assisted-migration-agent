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
	// V2V Pool Configuration (Heavyweight, Throttled Concurrency)
	defaultV2VWorkers = 1 // Throttled to 1 concurrent task to protect ESXi I/O
)

type InspectorServiceV2V struct {
	mu              sync.Mutex
	store           *store.Store2
	inspectionLimit int
	vddkLibDir      string
	credsSvc        *CredentialsService
	cfg             *config.Agent
	buildFn         inspectionBuilderFactory
	pool            *work.Pool2[models.InspectionStatus, models.InspectionResult]
}

func NewInspectorServiceV2V(st *store.Store2, inspectionLimit int, dataDir string, credsSvc *CredentialsService, cfg *config.Agent) *InspectorServiceV2V {
	return &InspectorServiceV2V{
		store:           st,
		inspectionLimit: inspectionLimit,
		vddkLibDir:      filepath.Join(dataDir, vddkFolder, vddkLibPath),
		credsSvc:        credsSvc,
		cfg:             cfg,
	}
}

func (i *InspectorServiceV2V) GetStatus() models.InspectorStatus {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.pool != nil && i.pool.IsRunning() {
		return models.InspectorStatus{State: models.InspectorStateRunning}
	}

	return models.InspectorStatus{State: models.InspectorStateReady}
}

func (i *InspectorServiceV2V) IsBusy() bool {
	return i.GetStatus().State == models.InspectorStateRunning
}

// Start dispatches VMs to v2v inspection pool.
// Pool is created on-demand per request batch to avoid re-entrant Pool2 execution limits.
func (i *InspectorServiceV2V) Start(ctx context.Context, vmIds []string) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(vmIds) > i.inspectionLimit {
		return srvErrors.NewInspectionLimitReachedError(i.inspectionLimit)
	}

	if i.pool != nil && i.pool.IsRunning() {
		return fmt.Errorf("the V2V inspection pool is busy processing previous inspection tasks")
	}

	creds, err := i.credsSvc.Resolve(ctx)
	if err != nil {
		return err
	}

	zap.S().Infow("starting v2v inspector", "vmCount", len(vmIds))

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
		zap.S().Named("inspector_v2v_service").Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	zap.S().Named("inspector_v2v_service").Info("vSphere connection established")

	defer func() {
		if err != nil {
			logoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			_ = vClient.Logout(logoutCtx)
		}
	}()

	// Track VMs we mark pending so a failure after pre-marking (e.g. pool.Start
	// error) does not strand them in "pending" forever with no pool driving them.
	var pendingMarked []string
	defer func() {
		if err == nil {
			return
		}
		for _, vmID := range pendingMarked {
			rbCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			if rbErr := i.store.InspectionV2V().Update(rbCtx, vmID, models.InspectionStatus{
				State: models.InspectionStateError,
				Error: err,
			}); rbErr != nil {
				zap.S().Named("inspector_v2v_service").Warnw("failed to roll back pending v2v status after start failure",
					"vmId", vmID, "error", rbErr)
			}
			cancel()
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
		V2VTimeout: i.v2vTimeout(),
	})
	if err != nil {
		return err
	}

	vmwareOperator := vmware.NewVMManager(vClient, creds.Username)

	buildFn := i.buildFn
	if buildFn == nil {
		buildFn = defaultV2VInspectionBuilderFactory(i.store, vmwareOperator, detector, i.cfg)
	}

	// Mark all VMs as pending in v2v status table
	for _, vmID := range vmIds {
		if err = i.store.InspectionV2V().Update(ctx, vmID, models.InspectionStatus{State: models.InspectionStatePending}); err != nil {
			return err
		}
		pendingMarked = append(pendingMarked, vmID)
	}

	// Provision and trigger V2V Pool (throttled to 1 worker)
	wb := make(map[string]work.WorkBuilder2[models.InspectionStatus, models.InspectionResult])
	for _, vmID := range vmIds {
		wb[vmID] = buildFn(vmID)
	}
	pool := work.NewPool2(wb).WithWorkers(defaultV2VWorkers, defaultV2VWorkers).
		WithSerialPipelines().
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

func (i *InspectorServiceV2V) Stop() error {
	// Grab the pool under the lock and clear it, then do the blocking teardown
	// OUTSIDE the lock. pool.Stop() blocks until finalize runs (RemoveSnapshot, a
	// vCenter round-trip that can take minutes on a degraded vCenter); holding
	// i.mu across it would wedge GetStatus/IsBusy/Start for that whole window.
	i.mu.Lock()
	pool := i.pool
	i.pool = nil
	i.mu.Unlock()

	if pool == nil {
		return srvErrors.NewInspectorNotRunningError()
	}

	return pool.Stop()
}

func (i *InspectorServiceV2V) Cancel(virtualMachineID string) error {
	// Snapshot the pool under the lock; run the blocking Cancel (which waits for
	// the per-VM finalize / RemoveSnapshot) outside the lock so status reads stay
	// responsive. The pool keeps running its other VMs, so we do not clear i.pool.
	i.mu.Lock()
	pool := i.pool
	i.mu.Unlock()

	if pool == nil || !pool.IsRunning() {
		return srvErrors.NewInspectorNotRunningError()
	}

	if _, err := pool.Cancel(virtualMachineID); err == nil {
		return nil
	}

	return srvErrors.NewResourceNotFoundError("vm", virtualMachineID)
}

// v2vTimeout returns the wall-clock timeout for a single v2v dry-run as a pointer
// for DetectorConfig. It ALWAYS returns a non-nil pointer (defaulting to 0 = no
// deadline) because a nil V2VTimeout makes the detector library fall back to its
// own 90m default — which would false-fail healthy-but-slow runs. The vCenter
// liveness health guard, not a fixed timeout, is the intended protection; 0 lets
// a run take as long as it needs. Operators may set a hard backstop via config.
func (i *InspectorServiceV2V) v2vTimeout() *time.Duration {
	d := time.Duration(0) // 0 → no wall-clock deadline
	if i.cfg != nil {
		d = i.cfg.V2VInspectionTimeout
	}
	return &d
}

func (i *InspectorServiceV2V) WithInspectionBuilder(builder inspectionBuilderFactory) *InspectorServiceV2V {
	i.buildFn = builder
	return i
}
