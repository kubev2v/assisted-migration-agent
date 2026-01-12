package services

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type CollectorService struct {
	scheduler *scheduler.Scheduler
	builder   models.WorkBuilder

	state models.CollectorStatus
	mu    sync.Mutex

	done   chan any
	cancel context.CancelFunc
}

func NewCollectorService(s *scheduler.Scheduler, builder models.WorkBuilder) *CollectorService {
	srv := &CollectorService{
		scheduler: s,
		builder:   builder,
		state:     models.CollectorStatus{State: models.CollectorStateReady},
	}

	return srv
}

// GetStatus returns the current collector status.
func (c *CollectorService) GetStatus() models.CollectorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state
}

// Start verifies creds with vCenter, and starts async collection.
func (c *CollectorService) Start(ctx context.Context, creds *models.Credentials) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isBusy() {
		return srvErrors.NewCollectionInProgressError()
	}

	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan any)

	c.state = models.CollectorStatus{State: models.CollectorStateConnecting}
	go c.run(runCtx, c.done, c.builder.WithCredentials(creds).Build())

	return nil
}

func (c *CollectorService) run(ctx context.Context, done chan any, work []models.WorkUnit) {
	defer close(done)
	defer func() {
		c.mu.Lock()
		if c.done == done {
			c.cancel = nil
			c.done = nil
		}
		c.mu.Unlock()
		zap.S().Debug("collector finished work")
	}()

	for len(work) > 0 {
		unit := work[0]
		work = work[1:]

		workFn := unit.Work()

		c.setState(unit.Status())

		future := c.scheduler.AddWork(func(ctx context.Context) (any, error) {
			return workFn(ctx)
		})

		zap.S().Debugw("collector changed state", "state", c.GetStatus().State)

		select {
		case <-ctx.Done():
			future.Stop()

			c.setState(models.CollectorStatus{State: models.CollectorStateReady})

			return
		case result := <-future.C():
			if result.Err != nil {
				c.setState(models.CollectorStatus{State: models.CollectorStateError, Error: result.Err})
				return
			}
			// TODO: Ideally, it's collector reposibility to save the inventory
			// Check if the result has an inventory and save it.
		}
	}
}

func (c *CollectorService) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		<-done
	}
}

func (c *CollectorService) setState(s models.CollectorStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = s
}

func (c *CollectorService) isBusy() bool {
	// must be protected by the caller
	switch c.state.State {
	case models.CollectorStateReady, models.CollectorStateCollected, models.CollectorStateError:
		return false
	default:
		return true
	}
}
