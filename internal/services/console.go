package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/cenkalti/backoff/v5"
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
	updateInterval      time.Duration
	agentID             uuid.UUID
	sourceID            uuid.UUID
	version             string
	state               *consoleState
	mu                  sync.Mutex // protects mode changes to prevent double run()
	scheduler           *scheduler.Scheduler
	client              *console.Client
	close               chan any
	collector           Collector
	inventoryLastHash   string // holds the hash of the last sent inventory
	store               *store.Store
	legacyStatusEnabled bool
}

func NewConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client, collector Collector, st *store.Store) (*Console, error) {
	targetStatus, err := models.ParseConsoleStatusType(cfg.Mode)
	if err != nil {
		targetStatus = models.ConsoleStatusDisconnected
	}

	defaultStatus := models.ConsoleStatus{
		Current: models.ConsoleStatusDisconnected,
		Target:  targetStatus,
	}

	config, err := st.Configuration().Get(context.Background())
	if err == nil {
		defaultStatus.Target = models.ConsoleStatusType(config.AgentMode)
	}

	c := newConsoleService(cfg, s, client, collector, st, defaultStatus)

	if err := c.store.Configuration().Save(context.Background(), &models.Configuration{AgentMode: models.AgentMode(defaultStatus.Target)}); err != nil {
		return nil, err
	}

	if defaultStatus.Target == models.ConsoleStatusConnected {
		go c.run()
	}

	zap.S().Named("console_service").Infow("agent mode", "current", defaultStatus.Current, "target", defaultStatus.Target)

	return c, nil
}

func newConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client, collector Collector, store *store.Store, defaultStatus models.ConsoleStatus) *Console {
	return &Console{
		updateInterval: cfg.UpdateInterval,
		agentID:        uuid.MustParse(cfg.ID),
		sourceID:       uuid.MustParse(cfg.SourceID),
		version:        cfg.Version,
		scheduler:      s,
		state: &consoleState{
			current: defaultStatus.Current,
			target:  defaultStatus.Target,
		},
		client:              client,
		store:               store,
		collector:           collector,
		legacyStatusEnabled: cfg.LegacyStatusEnabled,
	}
}

func (c *Console) GetMode(ctx context.Context) (models.AgentMode, error) {
	config, err := c.store.Configuration().Get(ctx)
	if err != nil {
		return models.AgentModeDisconnected, err
	}
	return config.AgentMode, nil
}

func (c *Console) SetMode(ctx context.Context, mode models.AgentMode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	prevMode, _ := c.GetMode(ctx)

	if prevMode == mode {
		return nil
	}

	if c.state.IsFatalStopped() {
		return errors.NewModeConflictError("console reporting stopped after receiving 401/410 from the server")
	}

	err := c.store.Configuration().Save(ctx, &models.Configuration{AgentMode: mode})
	if err != nil {
		return err
	}

	switch mode {
	case models.AgentModeConnected:
		c.state.SetTarget(models.ConsoleStatusConnected)
		zap.S().Debugw("starting run loop for connected mode")
		go c.run()
	case models.AgentModeDisconnected:
		c.state.SetTarget(models.ConsoleStatusDisconnected)
		zap.S().Debugw("stopping run loop for disconnected mode")
		c.close <- struct{}{}
	}

	zap.S().Named("console_service").Infow("agent mode changed", "mode", mode)
	return nil
}

func (c *Console) Status() models.ConsoleStatus {
	return c.state.Status()
}

// run is the main loop that sends status and inventory updates to the console.
//
// On each iteration:
//  1. Dispatch status and inventory updates (combined in single call) and block until complete.
//  2. Handle errors (fatal errors stop the loop, transient errors trigger backoff).
//  3. Wait for next tick or close signal.
//
// Fatal errors (stop the loop, no retry):
//   - ConsoleClientError (4xx): Client errors from console cannot be recovered.
//
// Transient errors are logged and stored in status.Error, but the loop continues.
// If inventory hasn't changed, the error state is preserved (not cleared or set).
//
// Backoff:
// When the server is unreachable (transient errors), exponential backoff is used to avoid
// hammering the server. On error, requests are skipped until the backoff interval expires.
// The interval grows exponentially from updateInterval up to 60 seconds max. On success,
// the backoff resets to allow immediate requests on the next tick.
func (c *Console) run() {
	c.state.SetCurrent(models.ConsoleStatusConnected)
	tick := time.NewTicker(c.updateInterval)
	c.close = make(chan any, 1)
	defer func() {
		tick.Stop()
		c.state.SetCurrent(models.ConsoleStatusDisconnected)
		zap.S().Named("console_service").Info("service stopped sending requests to console.rh.com")
		c.close = nil
	}()

	// use exponential backoff if server is unreachable.
	nextAllowedTime := time.Time{}
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = c.updateInterval
	b.MaxInterval = 60 * time.Second // Don't wait longer than 60s

	for {
		select {
		case <-tick.C:
		case <-c.close:
			return
		}

		now := time.Now()

		if !now.After(nextAllowedTime) {
			continue
		}

		future := c.dispatch()

		select {
		case result := <-future.C():
			if result.Err != nil {
				c.state.SetError(result.Err)
				// If the error from console.rh.com is 4xx stop the service
				// 4xx errors cannot be recovered and it is useless to keep sending requests
				if errors.IsConsoleClientError(result.Err) {
					zap.S().Named("console_service").Errorw("failed to send request to console. console service stopped", "error", result.Err.Error())
					c.state.SetFatalStopped()
					return
				}
				zap.S().Named("console_service").Errorw("failed to dispatch to console", "error", result.Err)
			} else {
				c.state.ClearError()
			}
		case <-c.close:
			future.Stop()
			return
		}

		// if there's an error activate backoff, otherwise reset it
		if c.state.GetError() != nil {
			nextAllowedTime = now.Add(b.NextBackOff())
			zap.S().Debugw("set backoff", "next-allowed-time", nextAllowedTime)
		} else {
			b.Reset()
			nextAllowedTime = time.Time{}
		}
	}
}

func (c *Console) Stop() {
	c.mu.Lock()
	closeCh := c.close
	c.mu.Unlock()

	if closeCh != nil {
		closeCh <- struct{}{}
	}
}

func (c *Console) dispatch() *scheduler.Future[scheduler.Result[any]] {
	return c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		collectorStatus := c.collector.GetStatus()
		status := string(collectorStatus.State)
		if c.legacyStatusEnabled {
			status = string(collectorStatus.State.ToV1())
		}
		statusInfo := status
		if collectorStatus.State == models.CollectorStateError {
			statusInfo = collectorStatus.Error.Error()
		}

		if err := c.client.UpdateAgentStatus(ctx, c.agentID, c.sourceID, c.version, status, statusInfo); err != nil {
			return nil, err
		}

		inventory, err := c.store.Inventory().Get(ctx)
		if err != nil {
			if errors.IsResourceNotFoundError(err) {
				return struct{}{}, nil
			}
			return nil, err
		}

		changed, err := c.isInventoryChanged(inventory)
		if err != nil {
			return nil, err
		}

		if !changed {
			return struct{}{}, nil
		}

		if err := c.client.UpdateSourceStatus(ctx, c.sourceID, c.agentID, *inventory); err != nil {
			return nil, err
		}

		zap.S().Named("console_service").Debugw("inventory updated", "hash", c.inventoryLastHash)

		return struct{}{}, nil
	})
}

func (c *Console) isInventoryChanged(inventory *models.Inventory) (bool, error) {
	data, err := json.Marshal(inventory)
	if err != nil {
		return false, fmt.Errorf("failed to marshal inventory: %w", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if hash == c.inventoryLastHash {
		return false, nil
	}

	c.inventoryLastHash = hash
	return true, nil
}

// consoleState holds the console status with its own mutex for thread-safe access.
// This separation prevents deadlocks between state updates (from run loop) and
// mode changes (from SetMode).
type consoleState struct {
	mu           sync.Mutex
	current      models.ConsoleStatusType
	target       models.ConsoleStatusType
	err          error
	fatalStopped bool
}

func (s *consoleState) Status() models.ConsoleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return models.ConsoleStatus{
		Current: s.current,
		Target:  s.target,
		Error:   s.err,
	}
}

func (s *consoleState) SetCurrent(c models.ConsoleStatusType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = c
}

func (s *consoleState) SetTarget(t models.ConsoleStatusType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target = t
}

func (s *consoleState) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *consoleState) ClearError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = nil
}

func (s *consoleState) GetError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *consoleState) SetFatalStopped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fatalStopped = true
}

func (s *consoleState) IsFatalStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatalStopped
}
