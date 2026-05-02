package work

import (
	"context"
	"errors"
	"fmt"
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type event[S any, R any] struct {
	PipelineID string
	Status     Status[S, R]
	IsDone     bool
}

type entry[S any, R any] struct {
	Status   Status[S, R]
	Done     bool
	CancelCh chan struct{}
	Pipeline *Pipeline2[S, R]
}

type Pool2[S any, R any] struct {
	mu              sync.Mutex
	sched           *scheduler.Scheduler[R]
	pipelines       map[string]entry[S, R]
	builders        map[string]WorkBuilder2[S, R]
	finalizeFn      func(ctx context.Context) error
	finalizeErr     error
	workers         int
	reservedWorkers int
	started         bool
	events          chan event[S, R]
	done            chan struct{}
}

func NewPool2[S any, R any](builders map[string]WorkBuilder2[S, R]) *Pool2[S, R] {
	return &Pool2[S, R]{
		builders:        builders,
		workers:         len(builders),
		reservedWorkers: len(builders),
		events:          make(chan event[S, R]),
	}
}

func (p *Pool2[S, R]) WithWorkers(normal, reserved int) *Pool2[S, R] {
	p.workers = normal
	p.reservedWorkers = reserved
	return p
}

func (p *Pool2[S, R]) WithFinalizer(fn func(ctx context.Context) error) *Pool2[S, R] {
	p.finalizeFn = fn
	return p
}

func (p *Pool2[S, R]) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.builders) == 0 {
		return errors.New("pool requires at least one builder")
	}

	if p.started {
		return srvErrors.NewServiceAlreadyStartedError()
	}

	sched, err := scheduler.NewScheduler[R](p.workers, p.reservedWorkers)
	if err != nil {
		return err
	}

	p.started = true
	p.sched = sched

	p.pipelines = make(map[string]entry[S, R], len(p.builders))

	for key, builder := range p.builders {
		pipeline := NewPipeline2(sched, builder)
		c, _ := pipeline.Start()
		p.pipelines[key] = entry[S, R]{Pipeline: pipeline}

		go func(pipelineID string, c chan Status[S, R], builder WorkBuilder2[S, R]) {
			var lastStatus Status[S, R]
			for s := range c {
				lastStatus = s
				p.events <- event[S, R]{PipelineID: pipelineID, Status: s}
			}

			future := sched.AddPriorityWork(func(ctx context.Context) (R, error) {
				return lastStatus.Result, builder.Finalize(ctx, lastStatus.Result)
			}, 1)
			res := <-future.C()
			if res.Err != nil {
				lastStatus.Err = res.Err
			}

			p.events <- event[S, R]{PipelineID: pipelineID, Status: lastStatus, IsDone: true}
		}(key, c, builder)
	}

	p.done = make(chan struct{})
	go p.run()

	return nil
}

func (p *Pool2[S, R]) Stop() error {
	p.mu.Lock()
	pipes := make([]*Pipeline2[S, R], 0, len(p.pipelines))
	for _, e := range p.pipelines {
		pipes = append(pipes, e.Pipeline)
	}
	s := p.sched
	done := p.done
	p.mu.Unlock()

	for _, pl := range pipes {
		pl.Stop()
	}

	if done != nil {
		<-done
	}

	if s != nil {
		s.Close()
	}

	return p.finalizeErr
}

func (p *Pool2[S, R]) Cancel(key string) Status[S, R] {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	if !ok || pl.Done {
		s := pl.Status
		p.mu.Unlock()
		return s
	}
	if pl.CancelCh == nil {
		pl.CancelCh = make(chan struct{})
		p.pipelines[key] = pl
	}
	done := pl.CancelCh
	p.mu.Unlock()

	pl.Pipeline.Stop()
	<-done

	p.mu.Lock()
	s := p.pipelines[key].Status
	p.mu.Unlock()
	return s
}

func (p *Pool2[S, R]) State(key string) (Status[S, R], error) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	p.mu.Unlock()

	if !ok {
		return Status[S, R]{}, fmt.Errorf("unknown key: %s", key)
	}

	return pl.Status, nil
}

func (p *Pool2[S, R]) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pl := range p.pipelines {
		if !pl.Done {
			return true
		}
	}

	return false
}

func (p *Pool2[S, R]) run() {
	defer func() { close(p.done) }()

	remaining := len(p.builders)
	for ev := range p.events {
		p.mu.Lock()
		e := p.pipelines[ev.PipelineID]
		e.Status = ev.Status
		if ev.IsDone {
			e.Done = true
			if e.CancelCh != nil {
				close(e.CancelCh)
			}
			remaining--
		}
		p.pipelines[ev.PipelineID] = e
		p.mu.Unlock()

		if remaining == 0 {
			break
		}
	}

	if p.finalizeFn != nil {
		future := p.sched.AddPriorityWork(func(ctx context.Context) (R, error) {
			var zero R
			return zero, p.finalizeFn(ctx)
		}, 1)
		res := <-future.C()
		p.finalizeErr = res.Err
	}
}
