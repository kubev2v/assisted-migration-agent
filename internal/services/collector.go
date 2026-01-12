package services

import (
	"context"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type CollectorService struct {
	scheduler *scheduler.Scheduler
	builder   models.WorkBuilder

	state         chan models.CollectorStatus
	cancelWork    chan any
	currentFuture *models.Future[models.Result[any]]
}

func NewCollectorService(s *scheduler.Scheduler, builder models.WorkBuilder) *CollectorService {
	srv := &CollectorService{
		scheduler:  s,
		builder:    builder,
		state:      make(chan models.CollectorStatus, 1),
		cancelWork: make(chan any),
	}

	srv.state <- models.CollectorStatus{State: models.CollectorStateReady}

	return srv
}

// GetStatus returns the current collector status.
func (c *CollectorService) GetStatus() models.CollectorStatus {
	s := <-c.state
	status := s
	c.state <- s

	return status
}

// Start verifies creds with vCenter, and starts async collection.
func (c *CollectorService) Start(ctx context.Context, creds *models.Credentials) error {
	switch c.GetStatus().State {
	case models.CollectorStateReady, models.CollectorStateCollected, models.CollectorStateError:
	default:
		return srvErrors.NewCollectionInProgressError()
	}

	<-c.state
	c.state <- models.CollectorStatus{State: models.CollectorStateReady}

	go c.run(c.builder.WithCredentials(creds).Build())

	// FIX: improve this hack.
	// What state should we return here because we cannot be sure the work started.
	// Or should we wait?
	<-c.state
	c.state <- models.CollectorStatus{State: models.CollectorStateConnecting}

	return nil
}

func (c *CollectorService) run(work []models.WorkUnit) {
	defer func() { zap.S().Debug("collector finished work") }()
	for len(work) > 0 {
		unit := work[0]
		work = work[1:]

		workFn := unit.Work()

		currentState := <-c.state
		c.state <- unit.Status()

		future := c.scheduler.AddWork(func(ctx context.Context) (any, error) {
			return workFn(ctx)
		})

		zap.S().Debugw("collector changed state", "state", currentState.State)

		select {
		case <-c.cancelWork:
			future.Stop()

			// back to ready state
			<-c.state
			c.state <- models.CollectorStatus{State: models.CollectorStateReady}

			c.cancelWork <- struct{}{}
			return
		case result := <-future.C():
			if result.Err != nil {
				<-c.state
				c.state <- models.CollectorStatus{State: models.CollectorStateError, Error: result.Err}
				return
			}
			// TODO: Ideally, it's collector reposibility to save the inventory
			// Check if the result has an inventory and save it.
		}
	}
}

func (c *CollectorService) Stop() {
	switch c.GetStatus().State {
	case models.CollectorStateReady, models.CollectorStateCollected, models.CollectorStateError:
		return
	default:
	}

	c.cancelWork <- struct{}{}
	<-c.cancelWork
}
