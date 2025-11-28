package models

import (
	"context"
	"sync"
)

type Future[T any] struct {
	input       chan T
	inputClosed bool
	value       T
	cancel      context.CancelFunc
	lock        sync.Mutex
}

func NewFuture[T any](input chan T, cancel context.CancelFunc) *Future[T] {
	f := &Future[T]{
		input:  input,
		cancel: cancel,
	}

	go func() {
		v := <-f.input
		f.lock.Lock()
		defer f.lock.Unlock()

		f.value = v
		f.inputClosed = true
		f.cancel()
	}()

	return f
}

func (f *Future[T]) Poll() (value T, isResolved bool) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.inputClosed {
		return f.value, true
	}

	var none T
	return none, false
}

func (f *Future[T]) Stop() {
	f.cancel()
}
