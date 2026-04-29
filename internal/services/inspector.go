package services

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"

	"github.com/kubev2v/assisted-migration-agent/internal/store"

	"github.com/vmware/govmomi"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// InspectorService orchestrates vCenter VM inspection: one asynchronous WorkPipeline per VM,
// a shared vSphere client for the run, and service-level status.
type InspectorService struct {
	mu              sync.Mutex
	cred            *models.Credentials
	vsphereClient   *govmomi.Client
	inspectionSvc   *inspectionService
	state           InspectorState
	stop            chan struct{}
	inspectionLimit int
	vddkLibDir      string
}

// NewInspectorService returns an idle inspector using the default inspection work units
// (validate, snapshot, inspect, save, remove snapshot).
// inspectionLimit is the maximum distinct VMs per cycle (Start batch + Add); non-positive means unlimited.
func NewInspectorService(s *store.Store, inspectionLimit int, dateDir string) *InspectorService {
	return &InspectorService{
		state: InspectorState{
			state: models.InspectorStateReady,
		},
		inspectionSvc:   newInspectionService(s),
		inspectionLimit: inspectionLimit,
		vddkLibDir:      filepath.Join(dateDir, vddkFolder, vddkLibPath),
	}
}

// GetStatus returns the current inspector status.
func (i *InspectorService) GetStatus() models.InspectorStatus {
	s := i.state.Status()
	if i.cred != nil {
		c := &models.Credentials{
			URL:      i.cred.URL,
			Username: i.cred.Username,
		}
		s.Credentials = c
	}
	return s
}

// GetVmStatus returns the inspection state for one VM from its WorkPipeline.
func (i *InspectorService) GetVmStatus(id string) models.InspectionStatus {
	return i.inspectionSvc.GetVmStatus(id)
}

// Start connects to vSphere, starts pipelines for each vmIDs entry, and launches the run loop.
func (i *InspectorService) Start(ctx context.Context, vmIDs []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.IsBusy() {
		return srvErrors.NewInspectionInProgressError()
	}

	if i.cred == nil {
		return srvErrors.NewCredentialsNotSetError()
	}

	if len(vmIDs) > i.inspectionLimit {
		return srvErrors.NewInspectionLimitReachedError(i.inspectionLimit)
	}

	i.state.Set(models.InspectorStateInitiating)
	zap.S().Infow("starting inspector", "vmCount", len(vmIDs))

	vClient, err := vmware.NewVsphereClient(ctx, i.cred.URL, i.cred.Username, i.cred.Password, true)
	if err != nil {
		zap.S().Named("inspector_service").Errorw("failed to connect to vSphere", "error", err)
		i.state.SetError(err)
		return err
	}

	zap.S().Named("inspector_service").Info("vSphere connection established")

	i.vsphereClient = vClient
	i.stop = make(chan struct{}, 1)

	detector, err := vmdetect.NewDetector(vmdetect.DetectorConfig{
		Credentials: vmdetect.Credentials{
			VCenterURL: i.cred.URL,
			Username:   i.cred.Username,
			Password:   i.cred.Password,
		},
		VDDKLibDir: i.vddkLibDir,
		Logger:     logrus.StandardLogger(),
	})
	if err != nil {
		return err
	}

	vmwareOperator := vmware.NewVMManager(i.vsphereClient, i.cred.Username)
	if err := i.inspectionSvc.Start(vmwareOperator, detector, vmIDs); err != nil {
		i.inspectionSvc.Stop()
		_ = i.closeVsphereClient(ctx)
		i.state.SetError(err)
		return err
	}

	go i.run(context.Background())

	return nil
}

func (i *InspectorService) Credentials(ctx context.Context, credentials models.Credentials) error {
	url, err := vmware.EnsureSdkSuffix(credentials.URL)
	if err != nil {
		return err
	}
	credentials.URL = url

	if err := vmware.VerifyCredentials(ctx, &credentials, "inspector"); err != nil {
		return srvErrors.NewVCenterError(err)
	}

	i.cred = &credentials
	return nil
}

// Stop requests cancellation of all VM pipelines and tears down the scheduler.
func (i *InspectorService) Stop() error {
	i.mu.Lock()

	if !i.IsBusy() {
		i.mu.Unlock()
		return srvErrors.NewInspectorNotRunningError()
	}

	i.inspectionSvc.Stop()
	stop := i.stop

	i.mu.Unlock()

	if stop != nil {
		stop <- struct{}{}
	}

	return nil
}

// Cancel stops the pipeline for a single VM ID. Returns InspectorNotRunningError if the service is idle.
func (i *InspectorService) Cancel(id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if !i.IsBusy() {
		return srvErrors.NewInspectorNotRunningError()
	}

	i.inspectionSvc.CancelVmInspection(id)

	return nil
}

// WithInspectionBuilder replaces the default per-VM work unit list.
func (i *InspectorService) WithInspectionBuilder(builder inspectionWorkBuilder) *InspectorService {
	i.inspectionSvc.WithWorkUnitsBuilder(builder)
	return i
}

// IsBusy reports whether the service is between Start and a terminal state (completed, canceled, error, ready).
func (i *InspectorService) IsBusy() bool {
	switch i.state.Status().State {
	case models.InspectorStateReady, models.InspectorStateCompleted, models.InspectorStateError, models.InspectorStateCanceled:
		return false
	default:
		return true
	}
}

// run marks Running, polls until no inspection pipeline is busy, then logs out and sets Completed or Canceled.
func (i *InspectorService) run(ctx context.Context) {
	i.state.Set(models.InspectorStateRunning)
	ticker := time.NewTicker(5 * time.Second)
	cancel := false

	defer func() {
		_ = i.closeVsphereClient(ctx)
		ticker.Stop()
		i.mu.Lock()
		i.stop = nil
		i.mu.Unlock() // don't send to channel if already closed
		if cancel {
			i.state.Set(models.InspectorStateCanceled)
		} else {
			i.state.Set(models.InspectorStateCompleted)
		}
	}()

	for {
		select {
		case <-i.stop:
			cancel = true
			return
		case <-ticker.C:
			if !i.inspectionSvc.IsBusy() {
				return
			}
		}
	}
}

func (i *InspectorService) closeVsphereClient(ctx context.Context) error {
	logoutCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	if i.vsphereClient != nil {
		return i.vsphereClient.Logout(logoutCtx)
	}

	return nil
}

// InspectorState holds the Inspector status with its own mutex for thread-safe access.
type InspectorState struct {
	mu    sync.Mutex
	state models.InspectorState
	err   error
}

func (s *InspectorState) Status() models.InspectorStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return models.InspectorStatus{
		State: s.state,
		Error: s.err,
	}
}

func (s *InspectorState) Set(st models.InspectorState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = st
	s.err = nil
}

func (s *InspectorState) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = models.InspectorStateError
	s.err = err
}
