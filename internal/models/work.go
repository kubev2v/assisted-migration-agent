package models

import "context"

// WorkUnit represents a single step in a work pipeline.
// S is the status type reported before execution.
// R is the result type threaded through the pipeline.
type WorkUnit[S any, R any] struct {
	Status func() S
	Work   func(ctx context.Context, result R) (R, error)
}
