package services

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/tupyy/assisted-migration-agent/internal/config"
	"github.com/tupyy/assisted-migration-agent/internal/models"
	"github.com/tupyy/assisted-migration-agent/pkg/console"
	"github.com/tupyy/assisted-migration-agent/pkg/scheduler"
)

type Console struct {
	updateInterval time.Duration
	agentID        string
	sourceID       string
	status         models.ConsoleStatus
	scheduler      *scheduler.Scheduler
	mu             sync.Mutex
	client         *console.Client
	close          chan any
}

func NewConnectedConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client) *Console {
	defaultStatus := models.ConsoleStatus{
		Current: models.ConsoleStatusDisconnected,
		Target:  models.ConsoleStatusConnected,
	}
	c := newConsoleService(cfg, s, client, defaultStatus)
	go c.run()
	return c
}

func NewConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client) *Console {
	defaultStatus := models.ConsoleStatus{
		Current: models.ConsoleStatusDisconnected,
		Target:  models.ConsoleStatusDisconnected,
	}
	return newConsoleService(cfg, s, client, defaultStatus)
}

func newConsoleService(cfg config.Agent, s *scheduler.Scheduler, client *console.Client, defaultStatus models.ConsoleStatus) *Console {
	c := &Console{
		updateInterval: cfg.UpdateInterval,
		agentID:        cfg.ID,
		sourceID:       cfg.SourceID,
		scheduler:      s,
		status:         defaultStatus,
		client:         client,
		close:          make(chan any),
	}
	return c
}

func (c *Console) SetMode(mode models.AgentMode) {
	c.mu.Lock()
	defer c.mu.Unlock()

	zap.S().Debugw("setting agent mode", "targetMode", mode, "currentTarget", c.status.Target)

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
}

func (c *Console) Status() models.ConsoleStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Console) run() {
	tick := time.NewTicker(c.updateInterval)
	defer func() {
		tick.Stop()
		zap.S().Debugw("run loop stopped")
	}()

	f := c.dispatchStatus()
	for {
		select {
		case <-tick.C:
		case <-c.close:
			zap.S().Debugw("close signal received, exiting run loop")
			return
		}

		result, isResolved := f.Poll()
		if isResolved {
			zap.S().Debugw("status update completed", "error", result.Err)
			f = c.dispatchStatus()
		}
	}
}

func (c *Console) dispatchStatus() *models.Future[models.Result[any]] {
	return c.scheduler.AddWork(func(ctx context.Context) (any, error) {
		return struct{}{}, c.client.UpdateAgentStatus(ctx, c.agentID, c.sourceID, models.CollectorStatusWaitingForCredentials)
	})
}
