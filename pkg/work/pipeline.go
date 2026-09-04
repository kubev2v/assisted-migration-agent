package work

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var ErrRunning = errors.New("pipeline is already running")

type Pipeline[S any, R any] struct {
	mu          sync.Mutex
	sched       *scheduler.Scheduler[R]
	workBuilder WorkBuilder[S, R]
	progress    progress[S, R]
	ticks       chan struct{}
	startCh     chan struct{}
	stop        chan struct{}
	done        chan struct{}
}

func NewPipeline[S any, R any](
	sched *scheduler.Scheduler[R],
	builder WorkBuilder[S, R],
) *Pipeline[S, R] {
	return &Pipeline[S, R]{
		sched:       sched,
		workBuilder: builder,
		startCh:     make(chan struct{}, 1),
	}
}

func (p *Pipeline[S, R]) Start() (chan struct{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.workBuilder == nil {
		return nil, errors.New("work builder cannot be null")
	}

	if p.sched == nil {
		return nil, errors.New("pipeline scheduler is required")
	}

	select {
	case p.startCh <- struct{}{}:
	default:
		return nil, ErrRunning
	}

	p.ticks = make(chan struct{})
	p.stop = make(chan struct{})
	p.done = make(chan struct{})

	stop := p.stop
	go func(builder WorkBuilder[S, R]) {
		defer func() {
			p.mu.Lock()
			p.stop = nil
			p.mu.Unlock()
			close(p.ticks)
			close(p.done)
		}()

	loop:
		for unit, hasMore := builder.Next(); hasMore; unit, hasMore = builder.Next() {
			p.progress.setState(unit.Status())

			select {
			case p.ticks <- struct{}{}:
			case <-stop:
				break loop
			}

			result, _ := p.progress.getResult()
			future := p.submit(unit, result)

			select {
			case <-stop:
				// TODO: drain future.C() and update result/progress so errors from cancelled work units are not lost
				future.Stop()
				break loop
			case res := <-future.C():
				p.progress.setResult(res.Data, res.Err)
				if res.Err != nil {
					select {
					case p.ticks <- struct{}{}:
					case <-stop:
					}
					break loop
				}
			}
		}

		result, _ := p.progress.getResult()
		future := p.sched.AddPriorityWork(func(ctx context.Context) (R, error) {
			return result, builder.Finalize(ctx, result)
		}, 1)

		res := <-future.C()
		if res.Err != nil {
			p.progress.setResult(res.Data, res.Err)
		}
	}(p.workBuilder)

	return p.ticks, nil
}

func (p *Pipeline[S, R]) State() S {
	return p.progress.getState()
}

func (p *Pipeline[S, R]) Result() (R, error) {
	return p.progress.getResult()
}

func (p *Pipeline[S, R]) Stop() {
	p.mu.Lock()
	done := p.done
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (p *Pipeline[S, R]) submit(u WorkUnit[S, R], result R) *scheduler.Future[scheduler.Result[R]] {
	return p.sched.AddWork(func(ctx context.Context) (R, error) {
		return u.Work(ctx, result)
	})
}

type progress[S any, R any] struct {
	mu     sync.Mutex
	state  S
	result R
	err    error
}

func (p *progress[S, R]) setState(s S) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
}

func (p *progress[S, R]) setResult(r R, err error) {
	p.mu.Lock()
	p.result = r
	p.err = err
	p.mu.Unlock()
}

func (p *progress[S, R]) getState() S {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *progress[S, R]) getResult() (R, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.err
}
