package scheduler

import (
	"context"

	"github.com/tupyy/assisted-migration-agent/internal/models"
)

type workRequest struct {
	fn  models.Work[any]
	c   chan models.Result[any]
	ctx context.Context
}

type worker struct {
	workFn models.Work[any]
	done   chan any
}

func (w worker) Work(r workRequest) {
	v, err := r.fn(r.ctx)
	r.c <- models.Result[any]{Data: v, Err: err}
	w.done <- struct{}{}
}

func newWorker(done chan any) worker {
	return worker{done: done}
}

type Scheduler struct {
	workers    *models.Queue[worker]
	workQueue  *models.Queue[workRequest]
	close      chan any
	done       chan any
	work       chan workRequest
	mainCtx    context.Context
	mainCancel context.CancelFunc
}

func NewScheduler(nbWorkers int) *Scheduler {
	done := make(chan any)
	wq := &models.Queue[worker]{}
	for range nbWorkers {
		wq.Push(newWorker(done))
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{
		workers:    wq,
		workQueue:  &models.Queue[workRequest]{},
		close:      make(chan any),
		done:       done,
		work:       make(chan workRequest),
		mainCtx:    ctx,
		mainCancel: cancel,
	}
	go s.run()
	return s
}

func (s *Scheduler) AddWork(w models.Work[any]) *models.Future[models.Result[any]] {
	c := make(chan models.Result[any])
	ctx, cancel := context.WithCancel(s.mainCtx)
	s.work <- workRequest{w, c, ctx}
	return models.NewFuture(c, cancel)
}

func (s *Scheduler) Close() {
	// TODO: find a way to wait for running workers
	s.mainCancel()
	s.close <- struct{}{}
}

func (s *Scheduler) run() {
	for {
		select {
		case w := <-s.work:
			s.workQueue.Push(w)
			if s.workers.Len() == 0 {
				continue
			}
			s.dispatch(s.workQueue.Pop())
		case <-s.done:
			s.workers.Push(newWorker(s.done))

			if s.workQueue.Len() == 0 {
				continue
			}
			s.dispatch(s.workQueue.Pop())
		case <-s.close:
			return
		}
	}
}

func (s *Scheduler) dispatch(r workRequest) {
	worker := s.workers.Pop()
	go worker.Work(r)
}
