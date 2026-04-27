package work

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var (
	ErrRunning = errors.New("pipeline is already running")
	ErrStopped = errors.New("pipeline is stopped")
)

type Status[S any, R any] struct {
	State  S
	Result R
	Err    error
}

type Pipeline[S any, R any] struct {
	mu          sync.Mutex
	sched       *scheduler.Scheduler[R]
	stop        chan struct{}
	done        chan struct{}
	workBuilder WorkBuilder[S, R]
	state       Status[S, R]
}

func NewPipeline[S any, R any](
	initialState S,
	sched *scheduler.Scheduler[R],
	builder WorkBuilder[S, R],
) *Pipeline[S, R] {
	return &Pipeline[S, R]{
		sched:       sched,
		workBuilder: builder,
		state:       Status[S, R]{State: initialState},
	}
}

func (p *Pipeline[S, R]) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.workBuilder == nil {
		return nil
	}

	if p.sched == nil {
		return errors.New("pipeline scheduler is required")
	}

	if p.done != nil {
		return ErrRunning
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	p.stop = stop
	p.done = done
	p.state.Err = nil

	go func(builder WorkBuilder[S, R], stop, done chan struct{}) {
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
				err = ErrStopped
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
				err = ErrStopped
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

func (p *Pipeline[S, R]) Stop() {
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

func (p *Pipeline[S, R]) State() Status[S, R] {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status[S, R]{
		State:  p.state.State,
		Result: p.state.Result,
		Err:    p.state.Err,
	}
}

func (p *Pipeline[S, R]) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done != nil
}

func (p *Pipeline[S, R]) submit(u WorkUnit[S, R], result R) *scheduler.Future[scheduler.Result[R]] {
	return p.sched.AddWork(func(ctx context.Context) (R, error) {
		return u.Work(ctx, result)
	})
}
