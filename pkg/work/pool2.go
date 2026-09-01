package work

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	// serial, when set, runs one pipeline at a time: the next builder's pipeline
	// is only launched after the current one has FULLY finished (all units plus
	// its Finalize). Pipelines that have not been launched yet have no entry, so a
	// queued key reports "pending" from the caller's own store until its turn.
	// Used by the v2v pool so two VMs never hold snapshots / drive vCenter at once.
	serial  bool
	order   []string
	nextIdx int
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

// WithSerialPipelines runs the builders one pipeline at a time instead of all at
// once. The next pipeline starts only after the current one fully completes
// (units + Finalize), so at most one VM is ever in-flight and the rest stay
// pending until their turn.
func (p *Pool2[S, R]) WithSerialPipelines() *Pool2[S, R] {
	p.serial = true
	return p
}

// launchPipeline creates, starts, and registers the pipeline for a single
// builder key, wiring its tick drain to emit a completion event. Callers must
// hold p.mu.
func (p *Pool2[S, R]) launchPipeline(key string) error {
	pipeline := NewPipeline2(p.sched, p.builders[key])
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

	return nil
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

	// Stable launch order so serial mode is deterministic (map ranging is random).
	p.order = make([]string, 0, len(p.builders))
	for key := range p.builders {
		p.order = append(p.order, key)
	}
	sort.Strings(p.order)

	if p.serial {
		// Launch only the first pipeline; run() launches the rest one at a time as
		// each completes. Queued keys have no pipeline yet and stay pending.
		if err := p.launchPipeline(p.order[0]); err != nil {
			return err
		}
		p.nextIdx = 1
	} else {
		for _, key := range p.order {
			if err := p.launchPipeline(key); err != nil {
				return err
			}
		}
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
		if _, isBuilder := p.builders[key]; !isBuilder {
			var empty S
			p.mu.Unlock()
			return empty, fmt.Errorf("unknown key: %s", key)
		}
		// Serial mode: the key is a valid builder that is still queued (no pipeline
		// yet). Launch it now so its Finalize runs and persists the terminal
		// (canceled) status; run() will skip relaunching it since it is now
		// registered. No snapshot exists yet, so the Stop below tears it down cheaply.
		if err := p.launchPipeline(key); err != nil {
			var empty S
			p.mu.Unlock()
			return empty, err
		}
		pl = p.pipelines[key]
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

		// Serial: the completed pipeline has fully finished (units + Finalize), so
		// it is now safe to start the next VM. Skip past any that fail to launch,
		// counting them as finished so the pool can still drain.
		for p.serial && p.nextIdx < len(p.order) {
			next := p.order[p.nextIdx]
			p.nextIdx++
			if _, exists := p.pipelines[next]; exists {
				// Already launched out-of-band (e.g. by Cancel); its own completion
				// event self-accounts, so skip without launching or double-counting.
				continue
			}
			if err := p.launchPipeline(next); err != nil {
				remaining--
				continue
			}
			break
		}
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
