package work_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

// fuzzBuilder is a WorkBuilder2 whose behavior is controlled by flags.
type fuzzBuilder struct {
	units      []work.WorkUnit[string, int]
	idx        int
	shouldFail bool
}

func (b *fuzzBuilder) Next() (work.WorkUnit[string, int], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[string, int]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *fuzzBuilder) Finalize(_ context.Context, _ int) error {
	if b.shouldFail {
		return errors.New("finalize error")
	}
	return nil
}

func FuzzPool2(f *testing.F) {
	f.Add(uint8(3), uint8(0b101), uint8(0b010), true, true)
	f.Add(uint8(1), uint8(0), uint8(0), false, false)
	f.Add(uint8(5), uint8(0b11111), uint8(0b00000), false, true)
	f.Add(uint8(4), uint8(0b0000), uint8(0b1111), true, false)

	f.Fuzz(func(t *testing.T, pipelineCount uint8, cancelMask uint8, errorMask uint8, finalizeErrors bool, callStop bool) {
		n := int(pipelineCount%8) + 1

		type pipelineBehavior struct {
			shouldError  bool
			shouldCancel bool
		}

		behaviors := make([]pipelineBehavior, n)
		for i := range n {
			behaviors[i] = pipelineBehavior{
				shouldError:  errorMask&(1<<(i%8)) != 0,
				shouldCancel: cancelMask&(1<<(i%8)) != 0,
			}
		}

		builders := make(map[string]work.WorkBuilder2[string, int], n)
		for i := range n {
			key := fmt.Sprintf("p%d", i)

			var units []work.WorkUnit[string, int]
			if behaviors[i].shouldError {
				units = []work.WorkUnit[string, int]{
					{
						Status: func() string { return "boom" },
						Work: func(_ context.Context, _ int) (int, error) {
							return 0, errors.New("work error")
						},
					},
				}
			} else {
				units = []work.WorkUnit[string, int]{
					{
						Status: func() string { return "step-1" },
						Work: func(_ context.Context, r int) (int, error) {
							return r + 1, nil
						},
					},
					{
						Status: func() string { return "step-2" },
						Work: func(_ context.Context, r int) (int, error) {
							return r + 10, nil
						},
					},
				}
			}

			builders[key] = &fuzzBuilder{
				units:      units,
				shouldFail: finalizeErrors && i == 0,
			}
		}

		pool := work.NewPool2[string, int](builders)
		if err := pool.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}

		var wg sync.WaitGroup
		for i := range n {
			if behaviors[i].shouldCancel {
				wg.Add(1)
				go func(key string) {
					defer wg.Done()
					_, _ = pool.Cancel(key)
				}(fmt.Sprintf("p%d", i))
			}
		}
		wg.Wait()

		if callStop {
			if err := pool.Stop(); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		} else {
			// Let it drain naturally, then stop.
			for pool.IsRunning() {
			}
			if err := pool.Stop(); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		}

		// Every pipeline must be done and reachable via State.
		for i := range n {
			key := fmt.Sprintf("p%d", i)
			if _, err := pool.State(key); err != nil {
				t.Errorf("State(%s): %v", key, err)
			}
		}

		if pool.IsRunning() {
			t.Error("pool still running after Stop")
		}

		// Double stop must not block.
		if err := pool.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	})
}
