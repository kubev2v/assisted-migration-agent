package services

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var (
	ErrCollectionInProgress = errors.New("collection already in progress")
	ErrInvalidState         = errors.New("invalid state for this operation")
	ErrInvalidCredentials   = errors.New("invalid credentials")
)

type CollectorService struct {
	scheduler  *scheduler.Scheduler
	store      *store.Store
	dataFolder string

	mu            sync.RWMutex
	state         models.CollectorState
	lastError     error
	collectFuture *models.Future[models.Result[any]]
}

func NewCollectorService(s *scheduler.Scheduler, st *store.Store, dataFolder string) *CollectorService {
	c := &CollectorService{
		scheduler:  s,
		store:      st,
		dataFolder: dataFolder,
		state:      models.CollectorStateReady,
	}

	// Log whether credentials exist from a previous run
	_, err := st.Credentials().Get(context.Background())
	if err == nil {
		zap.S().Info("collector initialized with existing credentials")
	} else {
		zap.S().Info("collector initialized, awaiting credentials")
	}

	return c
}

// GetStatus returns the current collector status.
func (c *CollectorService) GetStatus(ctx context.Context) models.CollectorStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := models.CollectorStatus{
		State: c.state,
	}

	if c.lastError != nil {
		status.Error = c.lastError.Error()
	}

	// Check if credentials exist
	_, err := c.store.Credentials().Get(ctx)
	status.HasCredentials = err == nil

	return status
}

// Start saves credentials, verifies them with vCenter, and starts async collection.
func (c *CollectorService) Start(ctx context.Context, creds *models.Credentials) error {
	// Check if collection is already in progress using the future
	c.mu.Lock()
	if c.collectFuture != nil && !c.collectFuture.IsResolved() {
		c.mu.Unlock()
		return ErrCollectionInProgress
	}
	c.mu.Unlock()

	// Save credentials
	if err := c.store.Credentials().Save(ctx, creds); err != nil {
		return err
	}

	// Set connecting state
	c.setState(models.CollectorStateConnecting)

	// Verify credentials synchronously
	if err := c.verifyCredentials(ctx, creds); err != nil {
		c.setError(err)
		return err
	}

	// Credentials verified, set connected
	c.setState(models.CollectorStateConnected)

	// Start async collection
	c.startCollectionJob()

	return nil
}

// Stop cancels any running collection but keeps credentials for retry.
func (c *CollectorService) Stop(ctx context.Context) error {
	// Cancel running job if any (this triggers context cancellation in the job)
	c.mu.Lock()
	if c.collectFuture != nil && !c.collectFuture.IsResolved() {
		c.collectFuture.Stop()
	}
	c.collectFuture = nil
	c.mu.Unlock()

	// Keep credentials - user can retry with same credentials
	// Reset state to ready
	c.setState(models.CollectorStateReady)
	return nil
}

// verifyCredentials tests the vCenter connection.
func (c *CollectorService) verifyCredentials(ctx context.Context, creds *models.Credentials) error {
	u, err := parseVCenterURL(creds)
	if err != nil {
		return err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	vimClient, err := vim25.NewClient(verifyCtx, soap.NewClient(u, true))
	if err != nil {
		return err
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}

	zap.S().Info("verifying vCenter credentials")
	if err := client.Login(verifyCtx, u.User); err != nil {
		if strings.Contains(err.Error(), "Login failure") ||
			(strings.Contains(err.Error(), "incorrect") && strings.Contains(err.Error(), "password")) {
			return ErrInvalidCredentials
		}
		return err
	}

	_ = client.Logout(verifyCtx)
	client.CloseIdleConnections()

	zap.S().Info("vCenter credentials verified successfully")
	return nil
}

func parseVCenterURL(creds *models.Credentials) (*url.URL, error) {
	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return nil, err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
	}
	u.User = url.UserPassword(creds.Username, creds.Password)
	return u, nil
}

// startCollectionJob starts the async inventory collection using the forklift collector.
func (c *CollectorService) startCollectionJob() {
	// Get credentials for the collector
	creds, err := c.store.Credentials().Get(context.Background())
	if err != nil {
		zap.S().Errorw("failed to get credentials for collection", "error", err)
		c.setError(err)
		return
	}

	c.collectFuture = c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		c.setState(models.CollectorStateCollecting)

		zap.S().Info("starting vSphere inventory collection")

		// Create the vSphere collector (local to this job)
		vsphereCollector, err := NewVSphereCollector(creds, c.dataFolder)
		if err != nil {
			zap.S().Errorw("failed to create vSphere collector", "error", err)
			c.setError(err)
			return nil, err
		}
		defer vsphereCollector.Close() // Ensure cleanup when job completes

		// Run the collection (use ctx from scheduler for cancellation)
		if err := vsphereCollector.Collect(ctx); err != nil {
			zap.S().Errorw("vSphere collection failed", "error", err)
			c.setError(err)
			return nil, err
		}

		zap.S().Infow("vSphere inventory collection completed", "db_path", vsphereCollector.DBPath())

		c.setState(models.CollectorStateCollected)

		// Transition back to ready after a brief moment
		time.Sleep(100 * time.Millisecond)
		c.setState(models.CollectorStateReady)

		return nil, nil
	})
}

// GetCredentials retrieves stored credentials.
func (c *CollectorService) GetCredentials(ctx context.Context) (*models.Credentials, error) {
	return c.store.Credentials().Get(ctx)
}

// HasCredentials checks if credentials exist.
func (c *CollectorService) HasCredentials(ctx context.Context) (bool, error) {
	_, err := c.store.Credentials().Get(ctx)
	if srvErrors.IsResourceNotFoundError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetInventory retrieves the stored inventory.
func (c *CollectorService) GetInventory(ctx context.Context) (*models.Inventory, error) {
	return c.store.Inventory().Get(ctx)
}

func (c *CollectorService) setState(state models.CollectorState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	zap.S().Debugw("collector state transition", "from", c.state, "to", state)
	c.state = state
	if state != models.CollectorStateError {
		c.lastError = nil
	}
}

func (c *CollectorService) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = models.CollectorStateError
	c.lastError = err
}
