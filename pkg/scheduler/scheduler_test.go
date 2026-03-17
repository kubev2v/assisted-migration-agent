package scheduler_test

import (
	"context"
	"runtime"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

var _ = Describe("Scheduler", func() {
	var s *scheduler.Scheduler[any]

	newScheduler := func(normalWorkers int, reservedWorkers int) *scheduler.Scheduler[any] {
		sched, err := scheduler.NewScheduler[any](normalWorkers, reservedWorkers)
		Expect(err).NotTo(HaveOccurred())
		return sched
	}

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
			s = newScheduler(1, 0)
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
			s = newScheduler(2, 0)
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
			s = newScheduler(1, 0)
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
			s = newScheduler(1, 0)
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
			s = newScheduler(4, 0)
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
			s = newScheduler(1, 0)
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
			s = newScheduler(1, 0)
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
			s = newScheduler(1, 0)
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
			s = newScheduler(1, 0)
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

	Context("Context propagation", func() {
		// Given a scheduler
		// When work is submitted
		// Then the work should receive a non-nil context
		It("should provide a valid context to work functions", func() {
			// Arrange
			s = newScheduler(1, 0)
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

	Context("Constructor validation", func() {
		It("should reject zero normal and zero reserved workers", func() {
			_, err := scheduler.NewScheduler[any](0, 0)
			Expect(err).To(HaveOccurred())
		})

		It("should reject negative normal workers", func() {
			_, err := scheduler.NewScheduler[any](-1, 0)
			Expect(err).To(HaveOccurred())
		})

		It("should reject negative reserved workers", func() {
			_, err := scheduler.NewScheduler[any](1, -1)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Reserved workers", func() {
		// Given a scheduler with one normal worker and no reserved workers
		// When lower-priority and higher-priority work are queued while the worker is busy
		// Then the higher-priority work should run first
		It("should execute higher-priority work first with only normal workers", func() {
			// Arrange
			s = newScheduler(1, 0)

			blocker := make(chan struct{})
			blockerStarted := make(chan struct{})
			order := make(chan string, 2)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(blockerStarted)
				<-blocker
				return nil, nil
			})
			Eventually(blockerStarted, 2*time.Second).Should(BeClosed())

			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				order <- "low"
				return "low", nil
			}, 1)
			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				order <- "high"
				return "high", nil
			}, 10)

			// Act
			close(blocker)

			// Assert
			var results []string
			for range 2 {
				Eventually(order, 2*time.Second).Should(Receive(
					Satisfy(func(v string) bool {
						results = append(results, v)
						return true
					}),
				))
			}
			Expect(results).To(Equal([]string{"high", "low"}))
		})

		// Given a scheduler with one normal worker and one reserved worker
		// When lower-priority and higher-priority work are both queued while both workers are busy
		// Then the higher-priority work should run first once the reserved worker becomes available
		It("should execute higher-priority work first with normal and reserved workers", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})
			blockReserved := make(chan struct{})
			reservedStarted := make(chan struct{})
			order := make(chan string, 2)
			var releaseNormal sync.Once
			var releaseReserved sync.Once
			defer releaseNormal.Do(func() { close(blockNormal) })
			defer releaseReserved.Do(func() { close(blockReserved) })

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				close(reservedStarted)
				<-blockReserved
				return nil, nil
			}, 100)
			Eventually(reservedStarted, 2*time.Second).Should(BeClosed())

			// Both workers are now occupied, so the next two priority jobs stay queued.
			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				order <- "low"
				return "low", nil
			}, 1)
			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				order <- "high"
				return "high", nil
			}, 10)

			// Act
			releaseReserved.Do(func() { close(blockReserved) })

			// Assert
			var results []string
			for range 2 {
				Eventually(order, 2*time.Second).Should(Receive(
					Satisfy(func(v string) bool {
						results = append(results, v)
						return true
					}),
				))
			}
			Expect(results).To(Equal([]string{"high", "low"}))

			releaseNormal.Do(func() { close(blockNormal) })
		})

		// Given a scheduler with one normal worker and one reserved worker
		// When normal-priority work and priority work are queued while the normal worker is busy
		// Then the priority work should run on the reserved worker before the normal work resumes
		It("should allow priority work to use reserved workers", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})
			priorityDone := make(chan struct{}, 1)
			normalDone := make(chan struct{}, 1)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			s.AddWork(func(ctx context.Context) (any, error) {
				normalDone <- struct{}{}
				return "normal", nil
			})

			// Act
			priorityFuture := s.AddPriorityWork(func(ctx context.Context) (any, error) {
				priorityDone <- struct{}{}
				return "priority", nil
			}, 1)

			// Assert
			Eventually(priorityDone, 2*time.Second).Should(Receive())
			Consistently(normalDone, 200*time.Millisecond).ShouldNot(Receive())

			var priorityResult scheduler.Result[any]
			Eventually(priorityFuture.C(), 2*time.Second).Should(Receive(&priorityResult))
			Expect(priorityResult.Err).NotTo(HaveOccurred())
			Expect(priorityResult.Data).To(Equal("priority"))

			close(blockNormal)
			Eventually(normalDone, 2*time.Second).Should(Receive())
		})

		// Given a scheduler configuration with only reserved workers
		// When we create the scheduler
		// Then it should return an error because reserved workers require at least one normal worker
		It("should reject schedulers with only reserved workers", func() {
			// Act
			_, err := scheduler.NewScheduler[any](0, 1)

			// Assert
			Expect(err).To(MatchError("scheduler requires at least one normal worker"))
		})

		// Given a scheduler with one normal worker and one reserved worker
		// When normal-priority work skips the reserved worker
		// Then the reserved worker should remain available for later priority work
		It("should keep reserved workers available after skipping normal work", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})
			priorityDone := make(chan struct{}, 1)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			s.AddWork(func(ctx context.Context) (any, error) {
				return "normal", nil
			})

			// Act
			priorityFuture := s.AddPriorityWork(func(ctx context.Context) (any, error) {
				priorityDone <- struct{}{}
				return "priority", nil
			}, 1)

			// Assert
			Eventually(priorityDone, 2*time.Second).Should(Receive())

			var priorityResult scheduler.Result[any]
			Eventually(priorityFuture.C(), 2*time.Second).Should(Receive(&priorityResult))
			Expect(priorityResult.Err).NotTo(HaveOccurred())
			Expect(priorityResult.Data).To(Equal("priority"))

			close(blockNormal)
		})

		// Given a scheduler with normal workers saturated and reserved workers idle
		// When normal-priority work is queued
		// Then it should wait for a normal worker rather than using the reserved worker
		It("should not run normal work on reserved workers", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})
			normalWorkDone := make(chan struct{}, 1)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			// Act
			s.AddWork(func(ctx context.Context) (any, error) {
				normalWorkDone <- struct{}{}
				return "normal", nil
			})

			// Assert - normal work should NOT run on the idle reserved worker
			Consistently(normalWorkDone, 300*time.Millisecond).ShouldNot(Receive())

			// Unblock and verify it eventually runs
			close(blockNormal)
			Eventually(normalWorkDone, 2*time.Second).Should(Receive())
		})

		// Given a scheduler with a busy worker
		// When normal work is queued first and then priority work is queued
		// Then priority work should run before the normal work
		It("should leapfrog normal work when priority work arrives later", func() {
			// Arrange
			s = newScheduler(1, 0)

			blocker := make(chan struct{})
			blockerStarted := make(chan struct{})
			order := make(chan string, 2)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(blockerStarted)
				<-blocker
				return nil, nil
			})
			Eventually(blockerStarted, 2*time.Second).Should(BeClosed())

			// Queue normal work first
			s.AddWork(func(ctx context.Context) (any, error) {
				order <- "normal"
				return nil, nil
			})
			// Then queue priority work
			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				order <- "priority"
				return nil, nil
			}, 5)

			// Act
			close(blocker)

			// Assert
			var results []string
			for range 2 {
				Eventually(order, 2*time.Second).Should(Receive(
					Satisfy(func(v string) bool {
						results = append(results, v)
						return true
					}),
				))
			}
			Expect(results).To(Equal([]string{"priority", "normal"}))
		})

		// Given a scheduler
		// When N items with the same priority are submitted
		// Then all should eventually complete without starvation
		It("should complete all same-priority work without starvation", func() {
			// Arrange
			s = newScheduler(2, 1)
			const n = 20
			futures := make([]*scheduler.Future[scheduler.Result[any]], n)

			// Act
			for i := range n {
				idx := i
				futures[idx] = s.AddPriorityWork(func(ctx context.Context) (any, error) {
					return idx, nil
				}, 5)
			}

			// Assert
			for i := range n {
				var result scheduler.Result[any]
				Eventually(futures[i].C(), 5*time.Second).Should(Receive(&result))
				Expect(result.Err).NotTo(HaveOccurred())
			}
		})

		// Given a scheduler
		// When AddPriorityWork is called with priority 0
		// Then it should behave like AddWork (no access to reserved workers)
		It("should treat priority 0 the same as AddWork", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})
			p0Done := make(chan struct{}, 1)

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			// Act
			s.AddPriorityWork(func(ctx context.Context) (any, error) {
				p0Done <- struct{}{}
				return nil, nil
			}, 0)

			// Assert - priority 0 should not use the reserved worker
			Consistently(p0Done, 300*time.Millisecond).ShouldNot(Receive())

			close(blockNormal)
			Eventually(p0Done, 2*time.Second).Should(Receive())
		})

		// Given a scheduler with a busy worker and queued work
		// When Close is called
		// Then queued work should not hang and no goroutines should leak
		It("should not hang on Close with queued but never-started work", func() {
			// Arrange
			s = newScheduler(1, 0)

			blocker := make(chan struct{})
			blockerStarted := make(chan struct{})

			s.AddWork(func(ctx context.Context) (any, error) {
				close(blockerStarted)
				<-blocker
				return nil, nil
			})
			Eventually(blockerStarted, 2*time.Second).Should(BeClosed())

			// Queue several items that will never start
			for range 5 {
				s.AddWork(func(ctx context.Context) (any, error) {
					return nil, nil
				})
			}

			// Act
			close(blocker)
			closeDone := make(chan struct{})
			go func() {
				s.Close()
				close(closeDone)
			}()

			// Assert
			Eventually(closeDone, 5*time.Second).Should(BeClosed())
			s = nil
		})

		// Given a closed scheduler
		// When AddPriorityWork is called
		// Then it should return a future with canceled error
		It("should return canceled when AddPriorityWork is called after Close", func() {
			// Arrange
			s = newScheduler(1, 1)
			s.Close()

			// Act
			future := s.AddPriorityWork(func(ctx context.Context) (any, error) {
				return "done", nil
			}, 5)

			// Assert
			var result scheduler.Result[any]
			Eventually(future.C(), 1*time.Second).Should(Receive(&result))
			Expect(result.Err).To(MatchError(context.Canceled))
			s = nil
		})

		// Given a scheduler
		// When Close is called concurrently from multiple goroutines
		// Then it should not panic
		It("should handle concurrent Close calls without panic", func() {
			// Arrange
			s = newScheduler(2, 1)
			for range 5 {
				s.AddWork(func(ctx context.Context) (any, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				})
			}

			// Act & Assert - should not panic
			var wg sync.WaitGroup
			for range 10 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					s.Close()
				}()
			}
			wg.Wait()
			s = nil
		})

		// Given a scheduler with reserved workers
		// When priority work panics on a reserved worker
		// Then the reserved worker should return to the pool and serve subsequent priority work
		It("should recover reserved worker after panic in priority work", func() {
			// Arrange
			s = newScheduler(1, 1)

			blockNormal := make(chan struct{})
			normalStarted := make(chan struct{})

			s.AddWork(func(ctx context.Context) (any, error) {
				close(normalStarted)
				<-blockNormal
				return nil, nil
			})
			Eventually(normalStarted, 2*time.Second).Should(BeClosed())

			// Act - panic on the reserved worker
			panicFuture := s.AddPriorityWork(func(ctx context.Context) (any, error) {
				panic("reserved worker panic")
			}, 1)

			var panicResult scheduler.Result[any]
			Eventually(panicFuture.C(), 2*time.Second).Should(Receive(&panicResult))
			Expect(panicResult.Err).To(HaveOccurred())
			Expect(panicResult.Err.Error()).To(ContainSubstring("worker panicked"))

			// Act - submit more priority work; the reserved worker should still be available
			nextFuture := s.AddPriorityWork(func(ctx context.Context) (any, error) {
				return "recovered", nil
			}, 1)

			// Assert
			var nextResult scheduler.Result[any]
			Eventually(nextFuture.C(), 2*time.Second).Should(Receive(&nextResult))
			Expect(nextResult.Err).NotTo(HaveOccurred())
			Expect(nextResult.Data).To(Equal("recovered"))

			close(blockNormal)
		})
	})
})
