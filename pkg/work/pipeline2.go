package work

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type Pipeline2[S any, R any] struct {
	mu          sync.Mutex
	sched       *scheduler.Scheduler[R]
	workBuilder WorkBuilder[S, R]
	state       chan Status[S, R]
	startCh     chan struct{}
	stop        chan struct{} // signals stop has been initiated
	done        chan struct{} // used to wait for run to return
}

func NewPipeline2[S any, R any](
	sched *scheduler.Scheduler[R],
	builder WorkBuilder[S, R],
) *Pipeline2[S, R] {
	return &Pipeline2[S, R]{
		sched:       sched,
		workBuilder: builder,
		startCh:     make(chan struct{}, 1),
	}
}

func (p *Pipeline2[S, R]) Start() (chan Status[S, R], error) {
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
		return nil, ErrRunning // should be either running or cannot restart
	}

	p.state = make(chan Status[S, R])
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	var result R

	stop := p.stop // copy to not block under mutex
	go func(builder WorkBuilder[S, R]) {
		defer func() {
			p.mu.Lock()
			p.stop = nil
			p.mu.Unlock()
			close(p.state)
			close(p.done)
		}()

		for unit, hasMore := builder.Next(); hasMore; unit, hasMore = builder.Next() {

			future := p.submit(unit, result)

			select {
			case <-stop:
				future.Stop()
				return
			case res := <-future.C():
				if res.Err != nil {
					p.state <- Status[S, R]{Err: res.Err, Result: result}
					return
				}
				result = res.Data
			}

			select {
			case p.state <- Status[S, R]{State: unit.Status(), Result: result}:
			case <-stop:
				return
			}
		}
	}(p.workBuilder)

	return p.state, nil
}

func (p *Pipeline2[S, R]) Stop() {
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

func (p *Pipeline2[S, R]) submit(u WorkUnit[S, R], result R) *scheduler.Future[scheduler.Result[R]] {
	return p.sched.AddWork(func(ctx context.Context) (R, error) {
		return u.Work(ctx, result)
	})
}
