package work

import (
	"fmt"
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type PoolEntry[S any, R any] struct {
	InitialState S
	Builder      WorkBuilder[S, R]
}

type Pool[S any, R any] struct {
	mu        sync.Mutex
	sched     *scheduler.Scheduler[R]
	pipelines map[string]*Pipeline[S, R]
	entries   map[string]PoolEntry[S, R]
	workers   int
	started   bool
}

func NewPool[S any, R any](workers int, entries map[string]PoolEntry[S, R]) *Pool[S, R] {
	return &Pool[S, R]{
		entries: entries,
		workers: workers,
	}
}

func (p *Pool[S, R]) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return srvErrors.NewServiceAlreadyStartedError()
	}
	p.started = true

	sched, err := scheduler.NewScheduler[R](p.workers, 0)
	if err != nil {
		return err
	}
	p.sched = sched

	p.pipelines = make(map[string]*Pipeline[S, R], len(p.entries))

	for key, entry := range p.entries {
		pipeline := NewPipeline(entry.InitialState, sched, entry.Builder)
		_ = pipeline.Start()
		p.pipelines[key] = pipeline
	}

	return nil
}

func (p *Pool[S, R]) Stop() {
	p.mu.Lock()
	pipelines := p.pipelines
	s := p.sched
	p.mu.Unlock()

	for _, pl := range pipelines {
		pl.Stop()
	}
	if s != nil {
		s.Close()
	}
}

func (p *Pool[S, R]) Cancel(key string) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	p.mu.Unlock()

	if ok {
		pl.Stop()
	}
}

func (p *Pool[S, R]) State(key string) (Status[S, R], error) {
	p.mu.Lock()
	pl, ok := p.pipelines[key]
	p.mu.Unlock()

	if !ok {
		return Status[S, R]{}, fmt.Errorf("unknown key: %s", key)
	}

	return pl.State(), nil
}

func (p *Pool[S, R]) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pl := range p.pipelines {
		if pl.IsRunning() {
			return true
		}
	}

	return false
}
