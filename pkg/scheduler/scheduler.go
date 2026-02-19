package scheduler

import (
	"context"
	"fmt"
	"sync"
)

type queue[T any] []T

func (wq *queue[T]) Len() int { return len(*wq) }

func (wq *queue[T]) Pop() T {
	old := *wq
	x := old[0]
	*wq = old[1:]
	return x
}

func (wq *queue[T]) Push(t T) {
	*wq = append(*wq, t)
}

type workRequest[T any] struct {
	fn  Work[T]
	c   chan Result[T]
	ctx context.Context
}

type worker[T any] struct {
	done chan any
	wg   *sync.WaitGroup
}

func (w worker[T]) Work(r workRequest[T]) {
	defer func() {
		if rec := recover(); rec != nil {
			r.c <- Result[T]{Err: fmt.Errorf("worker panicked: %v", rec)}
		}
		w.done <- struct{}{}
		w.wg.Done()
	}()

	v, err := r.fn(r.ctx)
	r.c <- Result[T]{Data: v, Err: err}
}

func newWorker[T any](done chan any, wg *sync.WaitGroup) worker[T] {
	return worker[T]{done: done, wg: wg}
}

type Scheduler[T any] struct {
	workers    *queue[worker[T]]
	workQueue  *queue[workRequest[T]]
	close      chan any
	done       chan any
	work       chan workRequest[T]
	mainCtx    context.Context
	mainCancel context.CancelFunc
	wg         sync.WaitGroup
	once       sync.Once
}

func NewDefaultScheduler(nbWorkers int) *Scheduler[any] {
	return NewScheduler[any](nbWorkers)
}

func NewScheduler[T any](nbWorkers int) *Scheduler[T] {
	done := make(chan any, nbWorkers)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler[T]{
		workers:    &queue[worker[T]]{},
		workQueue:  &queue[workRequest[T]]{},
		close:      make(chan any),
		done:       done,
		work:       make(chan workRequest[T]),
		mainCtx:    ctx,
		mainCancel: cancel,
	}
	for range nbWorkers {
		s.workers.Push(newWorker[T](done, &s.wg))
	}
	go s.run()
	return s
}

func (s *Scheduler[T]) AddWork(w Work[T]) *Future[Result[T]] {
	c := make(chan Result[T], 1)
	ctx, cancel := context.WithCancel(s.mainCtx)

	select {
	case <-s.mainCtx.Done():
		// we're closing here so send a result with an error
		c <- Result[T]{Err: context.Canceled}
	case s.work <- workRequest[T]{w, c, ctx}:
	}

	return NewFuture(c, cancel)
}

func (s *Scheduler[T]) Close() {
	s.once.Do(func() {
		s.mainCancel()
		s.close <- struct{}{}
		<-s.done
	})
}

func (s *Scheduler[T]) run() {
	defer close(s.done)
	for {
		select {
		case w := <-s.work:
			s.workQueue.Push(w)
			s.dispatch()
		case <-s.done:
			s.workers.Push(newWorker[T](s.done, &s.wg))
			s.dispatch()
		case <-s.close:
			s.wg.Wait()
			return
		}
	}
}

// dispatch drains the workQueue as much as possible
// based on available workers
func (s *Scheduler[T]) dispatch() {
	for s.workers.Len() > 0 && s.workQueue.Len() > 0 {
		r := s.workQueue.Pop()
		worker := s.workers.Pop()
		s.wg.Add(1)
		go worker.Work(r)
	}
}
