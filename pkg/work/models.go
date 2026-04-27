package work

import "context"

type WorkBuilder[S any, R any] interface {
	Next() (WorkUnit[S, R], bool)
}

// SliceWorkBuilder is a WorkBuilder backed by a fixed slice of work units.
type SliceWorkBuilder[S any, R any] struct {
	units []WorkUnit[S, R]
	idx   int
}

func NewSliceWorkBuilder[S any, R any](units []WorkUnit[S, R]) *SliceWorkBuilder[S, R] {
	return &SliceWorkBuilder[S, R]{units: units}
}

func (b *SliceWorkBuilder[S, R]) Next() (WorkUnit[S, R], bool) {
	if b.idx >= len(b.units) {
		return WorkUnit[S, R]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

// WorkUnit represents a single step in a work pipeline.
// S is the status type reported before execution.
// R is the result type threaded through the pipeline.
type WorkUnit[S any, R any] struct {
	Status func() S
	Work   func(ctx context.Context, result R) (R, error)
}
