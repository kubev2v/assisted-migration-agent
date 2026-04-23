package services

import (
	"context"
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

const (
	maxBackoffInterval        = 60 * time.Second
	initialState       string = "pending"
)

type Collector interface {
	GetStatus() models.CollectorStatus
}

type (
	consoleWorkUnit = models.WorkUnit[string, any]
)

type Console struct {
	updateInterval      time.Duration
	agentID             uuid.UUID
	sourceID            uuid.UUID
	version             string
	state               *consoleState
	mu                  sync.Mutex // protects mode changes to prevent double run()
	client              *console.Client
	requestBuilder      *console.RequestBuilder
	close               chan any
	collector           Collector
	eventSrv            *EventService
	store               *store.Store
	legacyStatusEnabled bool
}

func NewConsoleService(cfg config.Agent, client *console.Client, collector Collector, st *store.Store, eventSrv *EventService) (*Console, error) {
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

	c := newConsoleService(cfg, client, collector, st, eventSrv, defaultStatus)

	if err := c.store.Configuration().Save(context.Background(), &models.Configuration{AgentMode: models.AgentMode(defaultStatus.Target)}); err != nil {
		return nil, err
	}

	if defaultStatus.Target == models.ConsoleStatusConnected {
		c.close = make(chan any, 1)
		go c.run(c.close)
	}

	zap.S().Named("console_service").Infow("agent mode", "current", defaultStatus.Current, "target", defaultStatus.Target)

	return c, nil
}

func newConsoleService(cfg config.Agent, client *console.Client, collector Collector, store *store.Store, eventSrv *EventService, defaultStatus models.ConsoleStatus) *Console {
	agentID := uuid.MustParse(cfg.ID)
	sourceID := uuid.MustParse(cfg.SourceID)
	return &Console{
		updateInterval: cfg.UpdateInterval,
		agentID:        agentID,
		sourceID:       sourceID,
		version:        cfg.Version,
		state: &consoleState{
			current: defaultStatus.Current,
			target:  defaultStatus.Target,
		},
		client:              client,
		requestBuilder:      console.NewRequestBuilder(client, sourceID, agentID),
		store:               store,
		collector:           collector,
		eventSrv:            eventSrv,
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

	prevMode, err := c.GetMode(ctx)
	if err != nil {
		return err
	}

	if prevMode == mode {
		return nil
	}

	if c.state.IsFatalStopped() {
		return errors.NewModeConflictError("console reporting stopped after receiving 401/410 from the server")
	}

	if err := c.store.Configuration().Save(ctx, &models.Configuration{AgentMode: mode}); err != nil {
		return err
	}

	switch mode {
	case models.AgentModeConnected:
		c.state.SetTarget(models.ConsoleStatusConnected)
		zap.S().Debugw("starting run loop for connected mode")
		c.close = make(chan any, 1)
		go c.run(c.close)
	case models.AgentModeDisconnected:
		c.state.SetTarget(models.ConsoleStatusDisconnected)
		zap.S().Debugw("stopping run loop for disconnected mode")
		if c.close == nil {
			return nil
		}
		select {
		case c.close <- struct{}{}:
			<-c.close
		default:
			<-c.close
		}
		c.close = nil
	}

	zap.S().Named("console_service").Infow("agent mode changed", "mode", mode)
	return nil
}

func (c *Console) Status() models.ConsoleStatus {
	return c.state.Status()
}

// run is the main loop that delivers status updates and outbox events to the console.
//
// On each tick it creates a fresh pipeline by draining the outbox. The pipeline
// always starts with a status update unit. If events exist, RequestBuilder maps
// each one to an API call, and a final cleanup unit deletes the processed events.
// The scheduler is created once and shared across all pipelines in the loop.
//
// Loop structure:
//
//  1. Wait for the current interval or close signal.
//  2. If the pipeline is still running, skip this tick.
//  3. Once the pipeline finishes, process the result:
//     - Fatal error (4xx from console): stop the loop permanently.
//     - Transient error: double the interval (up to maxBackoffInterval).
//     - Success: reset the interval to updateInterval.
//  4. Create a new pipeline from the current outbox state and start it.
//
// This means transient error retries respect the backoff interval — the next
// pipeline is created and started only after the backoff wait, not before it.
// Events that failed delivery remain in the outbox and are picked up by the
// next pipeline.
//
// Shutdown:
//
// The close signal is checked on every iteration via the select. On exit,
// the deferred cleanup stops the pipeline, closes the scheduler, and sends
// an ack on closeCh. Stop() and SetMode use a non-blocking send to handle
// both normal shutdown (run alive) and self-exit (run already finished).
func (c *Console) run(closeCh chan any) {
	c.state.SetCurrent(models.ConsoleStatusConnected)

	sched, err := scheduler.NewScheduler[any](1, 0)
	if err != nil {
		c.state.SetError(err)
		return
	}

	var (
		pipeline    *WorkPipeline[string, any]
		errPipeline error
	)

	pipeline, errPipeline = c.createPipeline(sched)
	if errPipeline != nil {
		zap.S().Errorw("failed to create pipeline", "error", errPipeline)
		c.state.SetError(errPipeline)
		return
	}

	defer func() {
		if pipeline != nil {
			pipeline.Stop()
		}
		sched.Close()
		c.state.SetCurrent(models.ConsoleStatusDisconnected)
		zap.S().Named("console_service").Info("service stopped sending requests to console.rh.com")
		closeCh <- struct{}{}
	}()

	interval := c.updateInterval

	for {
		select {
		case <-time.After(interval):
		case <-closeCh:
			return
		}

		if pipeline.IsRunning() {
			continue
		}

		state := pipeline.State()
		if state.Err != nil {
			c.state.SetError(state.Err)
			if errors.IsConsoleClientError(state.Err) {
				zap.S().Named("console_service").Errorw("failed to send request to console. console service stopped", "error", state.Err.Error())
				c.state.SetFatalStopped()
				return
			}
			zap.S().Named("console_service").Errorw("failed to dispatch to console", "error", state.Err)
			interval = min(interval*2, maxBackoffInterval)
		} else {
			c.state.ClearError()
			interval = c.updateInterval
		}

		pipeline, errPipeline = c.createPipeline(sched)
		if errPipeline != nil {
			zap.S().Errorw("failed to create pipeline", "error", errPipeline)
			c.state.SetError(errPipeline)
			return
		}

		if err := pipeline.Start(); err != nil {
			c.state.SetError(err)
			return
		}
	}
}

func (c *Console) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.close == nil {
		return
	}

	select {
	case c.close <- struct{}{}:
		<-c.close
	default:
		<-c.close
	}
	c.close = nil
}

func (c *Console) createPipeline(s *scheduler.Scheduler[any]) (*WorkPipeline[string, any], error) {
	units := []consoleWorkUnit{
		{
			Status: func() string { return "status" },
			Work: func(ctx context.Context, r any) (any, error) {
				collectorStatus := c.collector.GetStatus()
				status := string(collectorStatus.State)
				if c.legacyStatusEnabled {
					status = string(collectorStatus.State.ToV1())
				}
				statusInfo := status
				if collectorStatus.State == models.CollectorStateError {
					statusInfo = collectorStatus.Error.Error()
				}
				return nil, c.client.UpdateAgentStatus(ctx, c.agentID, c.sourceID, c.version, status, statusInfo)
			},
		}}

	events, err := c.eventSrv.Events(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to read events: %w", err)
	}

	if len(events) == 0 {
		return NewWorkPipeline(initialState, s, units), nil
	}

	lastID := 0
	for _, e := range events {
		units = append(units, consoleWorkUnit{
			Status: func() string { return "event" },
			Work: func(ctx context.Context, r any) (any, error) {
				fn, err := c.requestBuilder.Build(e)
				if err != nil {
					if errors.IsUnknownEventKindError(err) {
						zap.S().Named("console_service").Warnw("skipping unknown event", "id", e.ID, "kind", e.Kind)
						return nil, nil
					}
					return nil, err
				}
				return nil, fn(ctx)
			},
		})
		lastID = e.ID
	}

	units = append(units, consoleWorkUnit{
		Status: func() string { return "clear" },
		Work: func(ctx context.Context, r any) (any, error) {
			return nil, c.eventSrv.Delete(ctx, lastID)
		},
	})

	return NewWorkPipeline(initialState, s, units), nil
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
