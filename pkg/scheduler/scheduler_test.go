package scheduler_test

import (
	"context"
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

	Describe("AddWork", func() {
		It("should add work and return a future", func() {
			s = scheduler.NewScheduler(1)

			work := func(ctx context.Context) (any, error) {
				return "done", nil
			}

			future := s.AddWork(work)
			Expect(future).NotTo(BeNil())

			Eventually(func() bool {
				return future.IsResolved()
			}, 2*time.Second, 100*time.Millisecond).Should(BeTrue())

			value := future.Result()
			Expect(value.Data).To(Equal("done"))
		})
	})

	Describe("Run work", func() {
		It("should execute multiple work items", func() {
			s = scheduler.NewScheduler(2)

			results := make(chan int, 3)
			for i := range 3 {
				idx := i
				work := func(ctx context.Context) (any, error) {
					results <- idx
					return idx, nil
				}
				s.AddWork(work)
			}

			Eventually(func() int {
				return len(results)
			}, 2*time.Second, 100*time.Millisecond).Should(Equal(3))
		})
	})

	Describe("Cancel work", func() {
		It("should cancel work via future.Stop()", func() {
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
			future.Stop()

			Eventually(cancelled, 2*time.Second).Should(Receive(BeTrue()))
		})

		It("should cancel work when scheduler is closed", func() {
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
			s.Close()
			s = nil // prevent AfterEach from closing again

			Eventually(cancelled, 2*time.Second).Should(Receive(BeTrue()))
		})
	})
})
