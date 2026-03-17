package scheduler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

type element[T any] struct {
	Priority int
	Data     T
}

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
	done       chan bool
	isReserved bool
	wg         *sync.WaitGroup
}

func (w worker[T]) Work(r workRequest[T]) {
	defer func() {
		if rec := recover(); rec != nil {
			r.c <- Result[T]{Err: fmt.Errorf("worker panicked: %v", rec)}
		}
		w.done <- w.isReserved
		w.wg.Done()
	}()

	v, err := r.fn(r.ctx)
	r.c <- Result[T]{Data: v, Err: err}
}

func newWorker[T any](done chan bool, wg *sync.WaitGroup, isReserved bool) worker[T] {
	return worker[T]{done: done, wg: wg, isReserved: isReserved}
}

type Scheduler[T any] struct {
	workers    *queue[worker[T]]
	workQueue  *queue[element[workRequest[T]]]
	close      chan any
	done       chan bool
	work       chan element[workRequest[T]]
	mainCtx    context.Context
	mainCancel context.CancelFunc
	wg         sync.WaitGroup
	once       sync.Once
}

func NewScheduler[T any](normalWorkers int, reservedWorkers int) (*Scheduler[T], error) {
	if normalWorkers < 1 {
		return nil, errors.New("scheduler requires at least one normal worker")
	}
	if reservedWorkers < 0 {
		return nil, errors.New("reserved workers cannot be negative")
	}

	done := make(chan bool, normalWorkers+reservedWorkers)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler[T]{
		workers:    &queue[worker[T]]{},
		workQueue:  &queue[element[workRequest[T]]]{},
		close:      make(chan any),
		done:       done,
		work:       make(chan element[workRequest[T]]),
		mainCtx:    ctx,
		mainCancel: cancel,
	}

	for range normalWorkers {
		s.workers.Push(newWorker[T](done, &s.wg, false))
	}

	for range reservedWorkers {
		s.workers.Push(newWorker[T](done, &s.wg, true))
	}

	go s.run()
	return s, nil
}

func (s *Scheduler[T]) AddWork(w Work[T]) *Future[Result[T]] {
	return s.addWork(w, 0)
}

func (s *Scheduler[T]) AddPriorityWork(w Work[T], priority int) *Future[Result[T]] {
	return s.addWork(w, priority)
}

func (s *Scheduler[T]) Close() {
	s.once.Do(func() {
		s.mainCancel()
		s.close <- struct{}{}
		s.wg.Wait()
	})
}

func (s *Scheduler[T]) run() {
	for {
		select {
		case w := <-s.work:
			s.workQueue.Push(w)
			s.dispatch()
		case isReserved := <-s.done:
			s.workers.Push(newWorker[T](s.done, &s.wg, isReserved))
			s.dispatch()
		case <-s.close:
			return
		}
	}
}

func (s *Scheduler[T]) addWork(w Work[T], priority int) *Future[Result[T]] {
	c := make(chan Result[T], 1)
	ctx, cancel := context.WithCancel(s.mainCtx)

	select {
	case <-s.mainCtx.Done():
		// we're closing here so send a result with an error
		c <- Result[T]{Err: context.Canceled}
	case s.work <- element[workRequest[T]]{Priority: priority, Data: workRequest[T]{w, c, ctx}}:
	}

	return NewFuture(c, cancel)
}

// dispatch drains the workQueue as much as possible
// based on available workers
func (s *Scheduler[T]) dispatch() {
	if s.workers.Len() == 0 || s.workQueue.Len() == 0 {
		return
	}

	slices.SortFunc(*s.workQueue, func(e1, e2 element[workRequest[T]]) int {
		if e1.Priority < e2.Priority {
			return 1
		}
		if e1.Priority > e2.Priority {
			return -1
		}
		return 0
	})

	// put reserved workers first
	slices.SortFunc(*s.workers, func(w1, w2 worker[T]) int {
		if !w1.isReserved && w2.isReserved {
			return 1
		}
		if w1.isReserved && !w2.isReserved {
			return -1
		}
		return 0
	})

	for s.workQueue.Len() > 0 {
		submitted := false
		work := s.workQueue.Pop()

		tmpList := make([]worker[T], 0, s.workers.Len())
		for s.workers.Len() > 0 {
			worker := s.workers.Pop()

			if worker.isReserved && work.Priority == 0 {
				tmpList = append(tmpList, worker)
				continue
			}

			s.wg.Add(1)
			go worker.Work(work.Data)
			submitted = true
			break
		}

		for _, worker := range tmpList {
			s.workers.Push(worker)
		}

		if !submitted {
			s.workQueue.Push(work)
			return
		}
	}
}
