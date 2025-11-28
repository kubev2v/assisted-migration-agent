package models

import "context"

type Work[T any] func(ctx context.Context) (T, error)

type Queue[T any] []T

func (wq *Queue[T]) Len() int { return len(*wq) }

func (wq *Queue[T]) Pop() T {
	old := *wq
	n := len(old)
	x := old[n-1]
	*wq = old[0 : n-1]
	return x
}

func (wq *Queue[T]) Push(t T) {
	*wq = append(*wq, t)
}

type Result[T any] struct {
	Data T
	Err  error
}
