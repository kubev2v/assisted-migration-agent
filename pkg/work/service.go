package work

import (
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

// Service is a one-time consumable executor for a single WorkBuilder.
// It owns a scheduler and a single Pipeline, handling their full lifecycle.
//
// Create → Start → read State / IsRunning → Stop (optional). The caller
// creates a new Service for each run; there is no restart.
type Service[S any, R any] struct {
	mu           sync.Mutex
	sched        *scheduler.Scheduler[R]
	pipeline     *Pipeline[S, R]
	initialState S
	builder      WorkBuilder[S, R]
	started      bool
}

func NewService[S any, R any](initialState S, builder WorkBuilder[S, R]) *Service[S, R] {
	return &Service[S, R]{
		initialState: initialState,
		builder:      builder,
	}
}

func (w *Service[S, R]) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return srvErrors.NewServiceAlreadyStartedError()
	}
	w.started = true

	sched, err := scheduler.NewScheduler[R](1, 0)
	if err != nil {
		return err
	}
	w.sched = sched

	w.pipeline = NewPipeline(w.initialState, sched, w.builder)
	if err := w.pipeline.Start(); err != nil {
		w.sched.Close()
		return err
	}

	return nil
}

func (w *Service[S, R]) Stop() {
	w.mu.Lock()
	p := w.pipeline
	s := w.sched
	w.mu.Unlock()

	if p != nil {
		p.Stop()
	}
	if s != nil {
		s.Close()
	}
}

func (w *Service[S, R]) State() Status[S, R] {
	w.mu.Lock()
	p := w.pipeline
	w.mu.Unlock()

	if p == nil {
		return Status[S, R]{State: w.initialState}
	}

	return p.State()
}

func (w *Service[S, R]) IsRunning() bool {
	w.mu.Lock()
	p := w.pipeline
	w.mu.Unlock()

	return p != nil && p.IsRunning()
}
