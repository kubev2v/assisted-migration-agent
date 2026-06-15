package work

import (
	"context"
	"errors"
	"fmt"
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type event struct {
	PipelineID string
}

type entry[S any, R any] struct {
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
	events          chan event
	done            chan struct{}
}

func NewPool2[S any, R any](builders map[string]WorkBuilder2[S, R]) *Pool2[S, R] {
	return &Pool2[S, R]{
		builders:        builders,
		workers:         len(builders),
		reservedWorkers: len(builders),
		events:          make(chan event),
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
		ticks, err := pipeline.Start()
		if err != nil {
			return fmt.Errorf("pipeline %s: %w", key, err)
		}
		p.pipelines[key] = entry[S, R]{Pipeline: pipeline}

		go func(pipelineID string, ticks chan struct{}) {
			for range ticks {
			}
			p.events <- event{PipelineID: pipelineID}
		}(key, ticks)
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

func (p *Pool2[S, R]) Cancel(key string) (S, error) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]

	if !ok {
		var empty S
		p.mu.Unlock()
		return empty, fmt.Errorf("unknown key: %s", key)
	}

	if pl.Done {
		s := pl.Pipeline.State()
		p.mu.Unlock()
		return s, nil
	}

	if pl.CancelCh == nil {
		pl.CancelCh = make(chan struct{})
		p.pipelines[key] = pl
	}
	done := pl.CancelCh
	p.mu.Unlock()

	pl.Pipeline.Stop()
	<-done

	return pl.Pipeline.State(), nil
}

func (p *Pool2[S, R]) State(key string) (S, error) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	p.mu.Unlock()

	if !ok {
		var empty S
		return empty, fmt.Errorf("unknown key: %s", key)
	}

	return pl.Pipeline.State(), nil
}

func (p *Pool2[S, R]) Result(key string) (R, error) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	p.mu.Unlock()

	if !ok {
		var empty R
		return empty, fmt.Errorf("unknown key: %s", key)
	}

	return pl.Pipeline.Result()
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
		e.Done = true
		if e.CancelCh != nil {
			close(e.CancelCh)
		}
		p.pipelines[ev.PipelineID] = e
		remaining--
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
