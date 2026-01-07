package services

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/collector"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type Processor interface {
	Process(ctx context.Context, c collector.Collector) error
}

type CollectorService struct {
	scheduler          *scheduler.Scheduler
	store              *store.Store
	collector          collector.Collector
	inventoryProcessor Processor

	mu            sync.RWMutex
	state         models.CollectorState
	lastError     error
	collectFuture *models.Future[models.Result[any]]
}

func NewCollectorService(s *scheduler.Scheduler, st *store.Store, c collector.Collector, p Processor) *CollectorService {
	srv := &CollectorService{
		scheduler:          s,
		store:              st,
		collector:          c,
		inventoryProcessor: p,
		state:              models.CollectorStateReady,
	}

	return srv
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
	c.mu.Lock()
	if c.collectFuture != nil && !c.collectFuture.IsResolved() {
		c.mu.Unlock()
		return srvErrors.NewCollectionInProgressError()
	}
	c.mu.Unlock()

	// Set connecting state
	c.setState(models.CollectorStateConnecting)

	// Verify credentials synchronously
	if err := c.collector.VerifyCredentials(ctx, creds); err != nil {
		c.setError(err)
		return err
	}

	// Save credentials
	if err := c.store.Credentials().Save(ctx, creds); err != nil {
		return err
	}

	c.setState(models.CollectorStateConnected)

	c.collectFuture = c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		c.setState(models.CollectorStateCollecting)

		zap.S().Named("collector_service").Info("starting vSphere inventory collection")

		defer c.collector.Close()

		if err := c.collector.Collect(ctx, creds); err != nil {
			zap.S().Named("collector_service").Errorw("vSphere collection failed", "error", err)
			c.setError(err)
			return nil, err
		}

		zap.S().Named("collector_service").Info("vSphere inventory collection completed")

		zap.S().Named("collector_service").Info("building inventory from collected data")
		if err := c.inventoryProcessor.Process(ctx, c.collector); err != nil {
			zap.S().Named("collector_service").Errorw("failed to build inventory", "error", err)
			c.setError(err)
			return nil, err
		}

		zap.S().Named("collector_service").Info("inventory successfully processed")

		c.setState(models.CollectorStateCollected)

		return nil, nil
	})

	return nil
}

func (c *CollectorService) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.collectFuture != nil && !c.collectFuture.IsResolved() {
		c.collectFuture.Stop()
	}
	c.collectFuture = nil
	c.mu.Unlock()

	c.setState(models.CollectorStateReady)
	return nil
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

// HasInventory checks if inventory exists.
func (c *CollectorService) HasInventory(ctx context.Context) (bool, error) {
	_, err := c.store.Inventory().Get(ctx)
	if srvErrors.IsResourceNotFoundError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *CollectorService) setState(state models.CollectorState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	zap.S().Named("collector_service").Debugw("collector state transition", "from", c.state, "to", state)
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
