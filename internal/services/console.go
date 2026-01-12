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
	status            models.ConsoleStatus
	scheduler         *scheduler.Scheduler
	mu                sync.Mutex
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
	c := &Console{
		updateInterval: cfg.UpdateInterval,
		agentID:        uuid.MustParse(cfg.ID),
		sourceID:       uuid.MustParse(cfg.SourceID),
		version:        cfg.Version,
		scheduler:      s,
		status:         defaultStatus,
		client:         client,
		close:          make(chan any),
		store:          store,
		collector:      collector,
	}
	return c
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
	defer c.mu.Unlock()

	switch mode {
	case models.AgentModeConnected:
		c.status.Target = models.ConsoleStatusConnected
		zap.S().Debugw("starting run loop for connected mode")
		go c.run()
	case models.AgentModeDisconnected:
		if c.status.Target == models.ConsoleStatusConnected {
			zap.S().Debugw("stopping run loop for disconnected mode")
			c.close <- struct{}{}
		}
		c.status.Target = models.ConsoleStatusDisconnected
	}

	zap.S().Named("console_service").Infow("agent mode changed", "current", mode, "target", c.status.Target)
}

func (c *Console) Status() models.ConsoleStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// run is the main loop that sends status and inventory updates to the console.
//
// On each tick (heartbeat):
//  1. Check if statusFuture is resolved. If yes, handle errors (fatal errors stop the loop),
//     then dispatch a new status update.
//  2. If collector status is not "collected", skip inventory processing.
//  3. If inventoryFuture is still pending, skip (don't send new inventory until previous completes).
//  4. If inventoryFuture resolved, handle any errors.
//  5. If inventory changed since last send (hash comparison), dispatch new inventory update.
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

	var inventoryFuture *models.Future[models.Result[any]]
	statusFuture := c.dispatchStatus()

	for {
		select {
		case <-tick.C:
		case <-c.close:
			return
		}

		if statusFuture != nil && statusFuture.IsResolved() {
			result := statusFuture.Result()
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
				c.status.Error = result.Err
			}
			statusFuture = c.dispatchStatus()
		}

		if inventoryFuture != nil {
			if !inventoryFuture.IsResolved() {
				continue
			}
			result := inventoryFuture.Result()
			if result.Err != nil {
				zap.S().Named("console_service").Errorw("failed to send inventory to console", "error", result.Err)
				c.status.Error = result.Err
			}
		}

		if inventory, changed, err := c.getInventoryIfChanged(context.TODO()); err == nil && changed {
			zap.S().Named("console_service").Infow("inventory changed. updating inventory...", "hash", c.inventoryLastHash)
			inventoryFuture = c.dispatchInventory(inventory)
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
