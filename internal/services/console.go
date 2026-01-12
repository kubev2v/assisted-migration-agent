package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type Collector interface {
	GetStatus() models.CollectorStatus
}

type Console struct {
	updateInterval    time.Duration
	agentID           uuid.UUID
	sourceID          uuid.UUID
	version           string
	state             models.ConsoleStatus
	mu                sync.Mutex
	scheduler         *scheduler.Scheduler
	client            *console.Client
	close             chan any
	collector         Collector
	inventoryLastHash string // holds the hash of the last sent inventory
	store             *store.Store
}

func NewConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client, collector Collector, st *store.Store) *Console {
	targetStatus, err := models.ParseConsoleStatusType(cfg.Mode)
	if err != nil {
		targetStatus = models.ConsoleStatusDisconnected
	}

	defaultStatus := models.ConsoleStatus{
		Current: models.ConsoleStatusDisconnected,
		Target:  targetStatus,
	}

	config, err := st.Configuration().Get(context.Background())
	if err == nil && config.AgentMode == models.AgentModeConnected {
		defaultStatus.Target = models.ConsoleStatusConnected
	}
	c := newConsoleService(cfg, s, client, collector, st, defaultStatus)

	if defaultStatus.Target == models.ConsoleStatusConnected {
		go c.run()
	}

	zap.S().Named("console_service").Infow("agent mode", "current", defaultStatus.Current, "target", defaultStatus.Target)

	return c
}

func newConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client, collector Collector, store *store.Store, defaultStatus models.ConsoleStatus) *Console {
	return &Console{
		updateInterval: cfg.UpdateInterval,
		agentID:        uuid.MustParse(cfg.ID),
		sourceID:       uuid.MustParse(cfg.SourceID),
		version:        cfg.Version,
		scheduler:      s,
		state:          defaultStatus,
		client:         client,
		close:          make(chan any),
		store:          store,
		collector:      collector,
	}
}

// IsDataSharingAllowed checks if the user has allowed data sharing.
func (c *Console) IsDataSharingAllowed(ctx context.Context) (bool, error) {
	config, err := c.store.Configuration().Get(ctx)
	if err != nil {
		return false, err
	}
	return config.AgentMode == models.AgentModeConnected, nil
}

func (c *Console) SetMode(mode models.AgentMode) {
	c.mu.Lock()
	prevTarget := c.state.Target

	switch mode {
	case models.AgentModeConnected:
		c.state.Target = models.ConsoleStatusConnected
		c.mu.Unlock()
		zap.S().Debugw("starting run loop for connected mode")
		go c.run()
	case models.AgentModeDisconnected:
		c.state.Target = models.ConsoleStatusDisconnected
		c.mu.Unlock()
		if prevTarget == models.ConsoleStatusConnected {
			zap.S().Debugw("stopping run loop for disconnected mode")
			c.close <- struct{}{}
		}
	default:
		c.mu.Unlock()
	}

	zap.S().Named("console_service").Infow("agent mode changed", "current", mode, "target", prevTarget)
}

func (c *Console) Status() models.ConsoleStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Console) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Error = err
}

// run is the main loop that sends status and inventory updates to the console.
//
// On each iteration:
//  1. Dispatch status update and block until complete. Handle errors (fatal errors stop the loop).
//  2. If inventory changed since last send (hash comparison), dispatch inventory update and block until complete.
//  3. Wait for next tick or close signal.
//
// Fatal errors (stop the loop, no retry):
//   - SourceGoneError (410): The source was deleted from the console. No point in sending updates.
//   - AgentUnauthorizedError (401): Invalid or expired JWT. Agent cannot authenticate.
//
// Transient errors are logged and stored in status.Error, but the loop continues.
func (c *Console) run() {
	tick := time.NewTicker(c.updateInterval)
	defer func() {
		tick.Stop()
		zap.S().Named("console_service").Info("service stopped sending requests to console.rh.com")
	}()

	for {
		select {
		case <-tick.C:
		case <-c.close:
			return
		}

		statusFuture := c.dispatchStatus()
		select {
		case result := <-statusFuture.C():
			if result.Err != nil {
				switch result.Err.(type) {
				case *errors.SourceGoneError:
					zap.S().Named("console_service").Error("source is gone..stop sending requests")
					return
				case *errors.AgentUnauthorizedError:
					zap.S().Named("console_service").Error("agent not authenticated..stop sending requests")
					return
				default:
					zap.S().Named("console_service").Errorw("failed to send status to console", "error", result.Err)
				}
				c.setError(result.Err)
			}
		case <-c.close:
			statusFuture.Stop()
			return
		}

		if inventory, changed, err := c.getInventoryIfChanged(context.TODO()); err == nil && changed {
			zap.S().Named("console_service").Infow("inventory changed. updating inventory...", "hash", c.inventoryLastHash)

			inventoryFuture := c.dispatchInventory(inventory)
			select {
			case result := <-inventoryFuture.C():
				if result.Err != nil {
					zap.S().Named("console_service").Errorw("failed to send inventory to console", "error", result.Err)
					c.setError(result.Err)
				}
			case <-c.close:
				inventoryFuture.Stop()
				return
			}
		}
	}
}

func (c *Console) dispatchStatus() *models.Future[models.Result[any]] {
	return c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		return struct{}{}, c.client.UpdateAgentStatus(ctx, c.agentID, c.sourceID, c.version, models.CollectorStatusType(c.collector.GetStatus().State))
	})
}

func (c *Console) dispatchInventory(inventory []byte) *models.Future[models.Result[any]] {
	return c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		return struct{}{}, c.client.UpdateSourceStatus(ctx, c.sourceID, bytes.NewReader(inventory))
	})
}

func (c *Console) getInventoryIfChanged(ctx context.Context) ([]byte, bool, error) {
	inventory, err := c.store.Inventory().Get(ctx)
	if err != nil {
		return nil, false, err
	}

	data, err := json.Marshal(inventory)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal inventory %v", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash == c.inventoryLastHash {
		return nil, false, nil
	}

	c.inventoryLastHash = hash
	return data, true, nil
}
