package scheduler_test

import (
	"context"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var _ = Describe("Scheduler", func() {
	var s *scheduler.Scheduler

	AfterEach(func() {
		if s != nil {
			s.Close()
		}
	})

	Context("AddWork", func() {
		// Given a scheduler with one worker
		// When we add work
		// Then it should return a future that eventually receives the result
		It("should add work and return a future", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			work := func(ctx context.Context) (any, error) {
				return "done", nil
			}

			// Act
			future := s.AddWork(work)

			// Assert
			Expect(future).NotTo(BeNil())
			var result scheduler.Result[any]
			Eventually(future.C(), 2*time.Second).Should(Receive(&result))
			Expect(result.Data).To(Equal("done"))
		})
	})

	Context("Run work", func() {
		// Given a scheduler with multiple workers
		// When we add multiple work items
		// Then all work items should be executed
		It("should execute multiple work items", func() {
			// Arrange
			s = scheduler.NewScheduler(2)
			results := make(chan int, 3)

			// Act
			for i := range 3 {
				idx := i
				work := func(ctx context.Context) (any, error) {
					results <- idx
					return idx, nil
				}
				s.AddWork(work)
			}

			// Assert
			Eventually(func() int {
				return len(results)
			}, 2*time.Second, 100*time.Millisecond).Should(Equal(3))
		})
	})

	Context("Cancel work", func() {
		// Given a scheduler with running work
		// When we call future.Stop()
		// Then the work should be cancelled via context
		It("should cancel work via future.Stop()", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			cancelled := make(chan bool, 1)
			work := func(ctx context.Context) (any, error) {
				select {
				case <-ctx.Done():
					cancelled <- true
					return nil, ctx.Err()
				case <-time.After(5 * time.Second):
					return "completed", nil
				}
			}
			future := s.AddWork(work)
			time.Sleep(100 * time.Millisecond)

			// Act
			future.Stop()

			// Assert
			Eventually(cancelled, 2*time.Second).Should(Receive(BeTrue()))
		})

		// Given a scheduler with running work
		// When we close the scheduler
		// Then all running work should be cancelled
		It("should cancel work when scheduler is closed", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			cancelled := make(chan bool, 1)
			work := func(ctx context.Context) (any, error) {
				select {
				case <-ctx.Done():
					cancelled <- true
					return nil, ctx.Err()
				case <-time.After(5 * time.Second):
					return "completed", nil
				}
			}
			s.AddWork(work)
			time.Sleep(100 * time.Millisecond)

			// Act
			s.Close()
			s = nil // prevent AfterEach from closing again

			// Assert
			Eventually(cancelled, 2*time.Second).Should(Receive(BeTrue()))
		})
	})

	Context("Goroutine cleanup", func() {
		// Given a scheduler under heavy load
		// When we close the scheduler
		// Then all goroutines should be cleaned up without leaks
		It("should not leak goroutines after Close under load", func() {
			// Arrange
			base := runtime.NumGoroutine()
			s = scheduler.NewScheduler(4)
			work := func(ctx context.Context) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			for range 200 {
				s.AddWork(work)
			}
			time.Sleep(100 * time.Millisecond)

			// Act
			s.Close()
			s = nil // prevent AfterEach from closing again

			// Assert
			Eventually(func() int {
				return runtime.NumGoroutine()
			}, 5*time.Second, 100*time.Millisecond).Should(BeNumerically("<=", base+10))
		})
	})

	Context("Close behavior", func() {
		// Given a closed scheduler
		// When we try to add work
		// Then it should return a future with canceled error
		It("should return canceled when AddWork is called after Close", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			s.Close()

			// Act
			future := s.AddWork(func(ctx context.Context) (any, error) {
				return "done", nil
			})

			// Assert
			var result scheduler.Result[any]
			Eventually(future.C(), 1*time.Second).Should(Receive(&result))
			Expect(result.Err).To(MatchError(context.Canceled))
		})

		// Given a scheduler with in-flight work
		// When we call Close
		// Then it should wait for in-flight work to finish
		It("should wait for in-flight work to finish on Close", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			started := make(chan struct{})
			unblock := make(chan struct{})
			work := func(ctx context.Context) (any, error) {
				close(started)
				<-unblock
				return "done", nil
			}
			s.AddWork(work)
			Eventually(started, 1*time.Second).Should(BeClosed())

			// Act
			closeDone := make(chan struct{})
			go func() {
				s.Close()
				close(closeDone)
			}()

			// Assert
			Consistently(closeDone, 200*time.Millisecond).ShouldNot(BeClosed())
			close(unblock)
			Eventually(closeDone, 1*time.Second).Should(BeClosed())
			s = nil // prevent AfterEach from closing again
		})
	})

	Context("Panic recovery", func() {
		// Given a work function that panics
		// When the scheduler executes it
		// Then the future should receive an error and the scheduler should continue working
		It("should recover from panics and return an error", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			panicWork := func(ctx context.Context) (any, error) {
				panic("something went wrong")
			}

			// Act
			future := s.AddWork(panicWork)

			// Assert
			var result scheduler.Result[any]
			Eventually(future.C(), 2*time.Second).Should(Receive(&result))
			Expect(result.Err).To(HaveOccurred())
			Expect(result.Err.Error()).To(ContainSubstring("worker panicked"))
		})

		// Given a work function that panics
		// When the scheduler recovers
		// Then it should be able to process subsequent work
		It("should continue processing after a panic", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			panicWork := func(ctx context.Context) (any, error) {
				panic("oops")
			}

			// Act - submit panic work and wait for it
			future := s.AddWork(panicWork)
			var result scheduler.Result[any]
			Eventually(future.C(), 2*time.Second).Should(Receive(&result))
			Expect(result.Err).To(HaveOccurred())

			// Act - submit normal work after the panic
			normalWork := func(ctx context.Context) (any, error) {
				return "recovered", nil
			}
			future2 := s.AddWork(normalWork)

			// Assert
			var result2 scheduler.Result[any]
			Eventually(future2.C(), 2*time.Second).Should(Receive(&result2))
			Expect(result2.Err).NotTo(HaveOccurred())
			Expect(result2.Data).To(Equal("recovered"))
		})
	})

	Context("FIFO ordering", func() {
		// Given a scheduler with 1 worker
		// When multiple work items are queued
		// Then they should execute in FIFO order
		It("should execute work in FIFO order with single worker", func() {
			// Arrange
			s = scheduler.NewScheduler(1)

			// Block the worker so we can queue up work
			blocker := make(chan struct{})
			s.AddWork(func(ctx context.Context) (any, error) {
				<-blocker
				return nil, nil
			})
			time.Sleep(50 * time.Millisecond) // let the worker pick up the blocker

			// Queue up 3 items while worker is busy
			order := make(chan int, 3)
			for i := 1; i <= 3; i++ {
				idx := i
				s.AddWork(func(ctx context.Context) (any, error) {
					order <- idx
					return nil, nil
				})
			}

			// Act - unblock the worker
			close(blocker)

			// Assert - items should come in order
			var results []int
			for range 3 {
				Eventually(order, 2*time.Second).Should(Receive(
					Satisfy(func(v int) bool {
						results = append(results, v)
						return true
					}),
				))
			}
			Expect(results).To(Equal([]int{1, 2, 3}))
		})
	})

	Context("Context propagation", func() {
		// Given a scheduler
		// When work is submitted
		// Then the work should receive a non-nil context
		It("should provide a valid context to work functions", func() {
			// Arrange
			s = scheduler.NewScheduler(1)
			var receivedCtx context.Context
			done := make(chan struct{})

			// Act
			s.AddWork(func(ctx context.Context) (any, error) {
				receivedCtx = ctx
				close(done)
				return nil, nil
			})

			// Assert
			Eventually(done, 2*time.Second).Should(BeClosed())
			Expect(receivedCtx).NotTo(BeNil())
			// Context should not already be cancelled for active work
			Expect(receivedCtx.Err()).To(BeNil())
		})
	})
})
