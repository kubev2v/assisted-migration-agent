package services

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var (
	errPipelineRunning = errors.New("pipeline is already running")
	errPipelineStopped = errors.New("pipeline is stopped")
)

// WorkPipeline is a generic executor that runs a sequence of WorkUnit[S, R]
// steps sequentially through a typed Scheduler. Each unit receives the result of
// the previous unit, forming a chain. S is the status type reported before each
// step; R is the result type threaded through.
//
// WorkPipeline owns only per-run execution state. The caller provides an
// initialState so State() is meaningful immediately after construction and
// Start(), even before the first unit is picked up by the scheduler. There are
// no callbacks; services observe state via State() and IsRunning().
//
// Scheduler ownership intentionally stays at the service layer. That keeps
// WorkPipeline generic, preserves type safety at the boundary where the result
// type is known, and allows multiple pipelines to share one typed scheduler and
// one concurrency budget.
//
// See `docs/scheduler-and-pipelines.md` for the broader rationale and usage
// guidance for services.
//
// Cancellation protocol:
//
// WorkPipeline uses two chan struct{} channels, both only closed (never sent on):
//   - stop: closed by Stop() to signal the goroutine to abort
//   - done: closed by the goroutine on exit to unblock Stop()
//
// When stopped, the pipeline reports errPipelineStopped (not context.Canceled),
// since cancellation is driven by the pipeline's own stop channel, not an
// external context.
//
// Usage:
//
//	p := NewWorkPipeline(initialState, sched, units)
//	err := p.Start()   // launches goroutine, returns errPipelineRunning if active
//	p.State()          // returns current status, result, and terminal error
//	p.Stop()           // signals stop and blocks until goroutine exits
//	p.IsRunning()      // true while the goroutine is active

type WorkPipelineStatus[S any, R any] struct {
	State  S
	Result R
	Err    error
}

type WorkPipeline[S any, R any] struct {
	mu          sync.Mutex
	sched       *scheduler.Scheduler[R]
	stop        chan struct{}
	done        chan struct{}
	workBuilder models.WorkBuilder[S, R]
	state       WorkPipelineStatus[S, R]
}

func NewWorkPipeline[S any, R any](
	initialState S,
	sched *scheduler.Scheduler[R],
	builder models.WorkBuilder[S, R],
) *WorkPipeline[S, R] {
	return &WorkPipeline[S, R]{
		sched:       sched,
		workBuilder: builder,
		state:       WorkPipelineStatus[S, R]{State: initialState},
	}
}

// Start begins executing units sequentially. Each unit is submitted to the
// scheduler as a separate work item. Returns errPipelineRunning if already
// active. The pipeline updates its internal state before each unit.
func (p *WorkPipeline[S, R]) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.workBuilder == nil {
		return nil
	}

	if p.sched == nil {
		return errors.New("pipeline scheduler is required")
	}

	if p.done != nil {
		return errPipelineRunning
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	p.stop = stop
	p.done = done
	p.state.Err = nil

	go func(builder models.WorkBuilder[S, R], stop, done chan struct{}) {
		var (
			result R
			err    error
		)

		defer func() {
			p.mu.Lock()
			p.stop = nil
			p.done = nil
			p.state.Result = result
			p.state.Err = err
			p.mu.Unlock()
			close(done)
		}()

		for unit, hasMore := builder.Next(); hasMore; unit, hasMore = builder.Next() {

			select {
			case <-stop:
				err = errPipelineStopped
				return
			default:
			}

			p.mu.Lock()
			p.state.State = unit.Status()
			p.state.Result = result
			future := p.submit(unit, result)
			p.mu.Unlock()

			select {
			case <-stop:
				future.Stop()
				err = errPipelineStopped
				return
			case res := <-future.C():
				if res.Err != nil {
					err = res.Err
					return
				}
				result = res.Data
			}
		}
	}(p.workBuilder, stop, done)

	return nil
}

// Stop cancels the currently executing work unit and blocks until the
// pipeline goroutine finishes. Safe to call when not running and safe
// to call concurrently.
func (p *WorkPipeline[S, R]) Stop() {
	p.mu.Lock()
	stop := p.stop
	done := p.done
	if stop != nil {
		close(stop)
		p.stop = nil
	}
	p.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (p *WorkPipeline[S, R]) State() WorkPipelineStatus[S, R] {
	p.mu.Lock()
	defer p.mu.Unlock()
	return WorkPipelineStatus[S, R]{
		State:  p.state.State,
		Result: p.state.Result,
		Err:    p.state.Err,
	}
}

// IsRunning returns whether the pipeline is currently executing.
func (p *WorkPipeline[S, R]) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done != nil
}

func (p *WorkPipeline[S, R]) submit(u models.WorkUnit[S, R], result R) *scheduler.Future[scheduler.Result[R]] {
	return p.sched.AddWork(func(ctx context.Context) (R, error) {
		return u.Work(ctx, result)
	})
}
