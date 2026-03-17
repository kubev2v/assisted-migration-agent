package services_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

func unit(status string, fn func(ctx context.Context, r int) (int, error)) models.WorkUnit[string, int] {
	return models.WorkUnit[string, int]{
		Status: func() string { return status },
		Work:   fn,
	}
}

var _ = Describe("WorkPipeline", func() {
	var sched *scheduler.Scheduler[int]

	newScheduler := func(normalWorkers int, reservedWorkers int) *scheduler.Scheduler[int] {
		s, err := scheduler.NewScheduler[int](normalWorkers, reservedWorkers)
		Expect(err).NotTo(HaveOccurred())
		return s
	}

	BeforeEach(func() {
		sched = newScheduler(1, 0)
	})

	AfterEach(func() {
		sched.Close()
	})

	Context("Start", func() {
		// Given a pipeline with no work units
		// When Start is called
		// Then it should return nil, preserve the initial state, and not be running
		It("should be a no-op for empty units", func() {
			// Arrange
			p := services.NewWorkPipeline[string, int]("pending", sched, nil)

			// Act
			err := p.Start()

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(p.IsRunning()).To(BeFalse())
			state := p.State()
			Expect(state.State).To(Equal("pending"))
			Expect(state.Result).To(Equal(0))
			Expect(state.Err).NotTo(HaveOccurred())
		})

		// Given a pipeline with units but no scheduler
		// When Start is called
		// Then it should return an error
		It("should return error when scheduler is nil", func() {
			// Arrange
			units := []models.WorkUnit[string, int]{
				unit("step", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			p := services.NewWorkPipeline[string, int]("pending", nil, units)

			// Act
			err := p.Start()

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given a pipeline that is already running
		// When Start is called a second time
		// Then it should return a pipeline-already-running error
		It("should return error on double start", func() {
			// Arrange
			gate := make(chan struct{})
			units := []models.WorkUnit[string, int]{
				unit("slow", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
					case <-ctx.Done():
					}
					return r, ctx.Err()
				}),
			}

			p := services.NewWorkPipeline("pending", sched, units)
			Expect(p.Start()).To(Succeed())

			// Act
			err := p.Start()

			// Assert
			Expect(err).To(MatchError("pipeline is already running"))

			close(gate)
			Eventually(p.IsRunning).Should(BeFalse())
		})
	})

	Context("sequential execution", func() {
		// Given a pipeline with three chained units (add-1, add-10, mul-2)
		// When the pipeline runs to completion
		// Then the result should be (0+1+10)*2 = 22 with no error
		It("should execute units in order and thread the result", func() {
			// Arrange
			units := []models.WorkUnit[string, int]{
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("mul-2", func(_ context.Context, r int) (int, error) { return r * 2, nil }),
			}

			p := services.NewWorkPipeline("pending", sched, units)

			// Act
			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			// Assert
			state := p.State()
			Expect(state.Err).NotTo(HaveOccurred())
			Expect(state.Result).To(Equal((0 + 1 + 10) * 2))
		})

		// Given a pipeline with two units
		// When the pipeline runs and the first unit blocks
		// Then the current state should be pulled from the pipeline while it is running
		It("should expose current state while running", func() {
			// Arrange
			gate := make(chan struct{})

			units := []models.WorkUnit[string, int]{
				unit("first", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r + 1, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
				unit("second", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := services.NewWorkPipeline("pending", sched, units)

			// Act
			Expect(p.Start()).To(Succeed())
			Eventually(func() string {
				return p.State().State
			}).Should(Equal("first"))
			Expect(p.State().Result).To(Equal(0))
			close(gate)
			Eventually(p.IsRunning).Should(BeFalse())

			// Assert
			state := p.State()
			Expect(state.State).To(Equal("second"))
			Expect(state.Result).To(Equal(2))
			Expect(state.Err).NotTo(HaveOccurred())
		})

		// Given a pipeline where the second of three units fails
		// When the pipeline runs
		// Then the error should be recorded and the third unit should never execute
		It("should stop on first error and report it via State", func() {
			// Arrange
			expectedErr := errors.New("unit-2 failed")
			var callCount atomic.Int32

			units := []models.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) {
					callCount.Add(1)
					return r + 1, nil
				}),
				unit("fail", func(_ context.Context, r int) (int, error) {
					callCount.Add(1)
					return r, expectedErr
				}),
				unit("never", func(_ context.Context, r int) (int, error) {
					callCount.Add(1)
					return r, nil
				}),
			}

			p := services.NewWorkPipeline("pending", sched, units)

			// Act
			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			// Assert
			Expect(p.State().Err).To(MatchError(expectedErr))
			Expect(callCount.Load()).To(Equal(int32(2)))
		})

		// Given a pipeline where a unit fails
		// When the pipeline finishes
		// Then State should expose the terminal error
		It("should expose terminal error through State", func() {
			// Arrange
			units := []models.WorkUnit[string, int]{
				{
					Status: func() string { return "failing" },
					Work:   func(_ context.Context, r int) (int, error) { return r, errors.New("boom") },
				},
			}

			p := services.NewWorkPipeline("pending", sched, units)

			// Act
			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			// Assert
			state := p.State()
			Expect(state.State).To(Equal("failing"))
			Expect(state.Err).To(MatchError("boom"))
		})
	})

	Context("Stop", func() {
		// Given a pipeline that has not been started
		// When Stop is called
		// Then it should not panic
		It("should be safe to call when not running", func() {
			// Arrange
			units := []models.WorkUnit[string, int]{
				unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			p := services.NewWorkPipeline("pending", sched, units)

			// Act & Assert
			Expect(func() { p.Stop() }).NotTo(Panic())
		})

		// Given a pipeline with a blocking unit that respects context cancellation
		// When Stop is called while the unit is running
		// Then the pipeline should report errPipelineStopped and stop
		It("should cancel a running pipeline", func() {
			// Arrange
			gate := make(chan struct{})
			units := []models.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r + 1, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
				unit("never", func(_ context.Context, r int) (int, error) {
					return r + 100, nil
				}),
			}

			p := services.NewWorkPipeline("pending", sched, units)
			Expect(p.Start()).To(Succeed())
			Expect(p.IsRunning()).To(BeTrue())

			// Act
			p.Stop()

			// Assert
			Expect(p.State().Err).To(MatchError("pipeline is stopped"))
			Expect(p.IsRunning()).To(BeFalse())
		})

		// Given a pipeline that was stopped
		// When a new pipeline is created on the same scheduler and started
		// Then it should run to completion
		It("should allow restart after stop", func() {
			// Arrange
			gate := make(chan struct{})
			makeUnits := func() []models.WorkUnit[string, int] {
				return []models.WorkUnit[string, int]{
					unit("work", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r + 1, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				}
			}

			p1 := services.NewWorkPipeline("pending", sched, makeUnits())
			Expect(p1.Start()).To(Succeed())
			p1.Stop()
			Expect(p1.IsRunning()).To(BeFalse())

			p2 := services.NewWorkPipeline("pending", sched, makeUnits())

			// Act
			Expect(p2.Start()).To(Succeed())
			close(gate)

			// Assert
			Eventually(p2.IsRunning).Should(BeFalse())
			Expect(p2.State().Err).NotTo(HaveOccurred())
			Expect(p2.State().Result).To(Equal(1))
		})

		// Given a pipeline with a fast unit that completes almost instantly
		// When Stop is called right after Start (racing with natural completion)
		// Then Stop should return without deadlocking
		It("should not deadlock when stop races with natural completion", func() {
			// Arrange
			units := []models.WorkUnit[string, int]{
				unit("fast", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := services.NewWorkPipeline("pending", sched, units)
			Expect(p.Start()).To(Succeed())
			time.Sleep(5 * time.Millisecond)

			// Act
			stopDone := make(chan struct{})
			go func() {
				p.Stop()
				close(stopDone)
			}()

			// Assert
			Eventually(stopDone, 2*time.Second).Should(BeClosed())
			Expect(p.IsRunning()).To(BeFalse())
		})
	})

	Context("multiple pipelines on the same scheduler", func() {
		// Given two pipelines sharing a 4-worker scheduler, each with two chained units
		// When both pipelines are started concurrently
		// Then both should complete with their own correct results
		It("should run two pipelines concurrently on a multi-worker scheduler", func() {
			// Arrange
			multiSched := newScheduler(4, 0)
			defer multiSched.Close()

			pipelines := make([]*services.WorkPipeline[string, int], 2)

			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				offset := (i + 1) * 100

				units := []models.WorkUnit[string, int]{
					unit("step-a", func(_ context.Context, r int) (int, error) { return r + offset, nil }),
					unit("step-b", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				}

				pipelines[i] = services.NewWorkPipeline("pending", multiSched, units)

				go func(p *services.WorkPipeline[string, int]) {
					defer wg.Done()
					Expect(p.Start()).To(Succeed())
				}(pipelines[i])
			}

			// Act & Assert
			wg.Wait()
			for i, p := range pipelines {
				Eventually(p.IsRunning).Should(BeFalse())
				state := p.State()
				Expect(state.Err).NotTo(HaveOccurred())
				Expect(state.Result).To(Equal((i+1)*100 + 1))
			}
		})

		// Given a slow pipeline and a fast pipeline sharing a 4-worker scheduler
		// When the slow pipeline is stopped while the fast pipeline is running
		// Then the fast pipeline should complete successfully and the slow one should be canceled
		It("should allow stopping one pipeline without affecting the other", func() {
			// Arrange
			multiSched := newScheduler(4, 0)
			defer multiSched.Close()

			gate := make(chan struct{})

			slowUnits := []models.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r + 1, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
			}

			fastUnits := []models.WorkUnit[string, int]{
				unit("fast-a", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("fast-b", func(_ context.Context, r int) (int, error) { return r + 20, nil }),
			}

			pSlow := services.NewWorkPipeline("pending", multiSched, slowUnits)
			pFast := services.NewWorkPipeline("pending", multiSched, fastUnits)

			Expect(pSlow.Start()).To(Succeed())
			Expect(pFast.Start()).To(Succeed())

			// Act
			Eventually(pFast.IsRunning).Should(BeFalse())
			pSlow.Stop()

			// Assert
			Expect(pFast.State().Err).NotTo(HaveOccurred())
			Expect(pFast.State().Result).To(Equal(30))
			Expect(pSlow.State().Err).To(MatchError("pipeline is stopped"))
		})

		// Given two pipelines sharing a 2-worker scheduler, one that fails and one that succeeds
		// When both pipelines run concurrently
		// Then each pipeline should report its own result independently
		It("should isolate errors between pipelines on the same scheduler", func() {
			// Arrange
			multiSched := newScheduler(2, 0)
			defer multiSched.Close()

			failUnits := []models.WorkUnit[string, int]{
				unit("fail", func(_ context.Context, _ int) (int, error) {
					return 0, errors.New("boom")
				}),
			}

			okUnits := []models.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) { return r + 42, nil }),
			}

			pFail := services.NewWorkPipeline("pending", multiSched, failUnits)
			pOk := services.NewWorkPipeline("pending", multiSched, okUnits)

			// Act
			Expect(pFail.Start()).To(Succeed())
			Expect(pOk.Start()).To(Succeed())

			// Assert
			Eventually(pFail.IsRunning).Should(BeFalse())
			Eventually(pOk.IsRunning).Should(BeFalse())

			Expect(pFail.State().Err).To(MatchError("boom"))
			Expect(pOk.State().Err).NotTo(HaveOccurred())
			Expect(pOk.State().Result).To(Equal(42))
		})
	})

	Context("stress", func() {
		// Given 10 goroutines each creating a pipeline, starting it, and immediately stopping it
		// When all goroutines run concurrently on a shared scheduler
		// Then no goroutine should panic or deadlock, and all pipelines should end up not running
		It("should handle start + immediate stop without races", func() {
			// Arrange
			stressSched := newScheduler(4, 0)
			defer stressSched.Close()

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			// Act
			for i := range n {
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()

					units := []models.WorkUnit[string, int]{
						unit("blocking", func(ctx context.Context, r int) (int, error) {
							select {
							case <-ctx.Done():
								return r, ctx.Err()
							case <-time.After(time.Second):
								return r + idx, nil
							}
						}),
					}

					p := services.NewWorkPipeline("pending", stressSched, units)

					Expect(p.Start()).To(Succeed())
					p.Stop()

					// Assert
					Expect(p.IsRunning()).To(BeFalse())
				}(i)
			}

			// Assert
			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
		})

		// Given a single running pipeline with a blocking unit
		// When 10 goroutines all call Stop concurrently
		// Then no goroutine should panic or deadlock
		It("should handle concurrent Stop calls without races", func() {
			// Arrange
			stressSched := newScheduler(1, 0)
			defer stressSched.Close()

			units := []models.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-ctx.Done():
						return r, ctx.Err()
					case <-time.After(5 * time.Second):
						return r, nil
					}
				}),
			}

			p := services.NewWorkPipeline("pending", stressSched, units)
			Expect(p.Start()).To(Succeed())

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			// Act
			for range n {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					p.Stop()
				}()
			}

			// Assert
			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
			Expect(p.IsRunning()).To(BeFalse())
		})

		// Given 10 independent pipelines sharing a 4-worker scheduler, each with 2 fast units
		// When all pipelines are started concurrently
		// Then all 10 should complete with correct results
		It("should run 10 independent pipelines concurrently without races", func() {
			// Arrange
			stressSched := newScheduler(4, 0)
			defer stressSched.Close()

			const n = 10
			pipelines := make([]*services.WorkPipeline[string, int], n)

			var wg sync.WaitGroup
			wg.Add(n)

			// Act
			for i := range n {
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()

					offset := (idx + 1) * 10
					units := []models.WorkUnit[string, int]{
						unit("step-a", func(_ context.Context, r int) (int, error) { return r + offset, nil }),
						unit("step-b", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
					}

					pipelines[idx] = services.NewWorkPipeline("pending", stressSched, units)
					Expect(pipelines[idx].Start()).To(Succeed())
				}(i)
			}

			// Assert
			wg.Wait()
			for i := range n {
				Eventually(pipelines[i].IsRunning).Should(BeFalse())
				state := pipelines[i].State()
				Expect(state.Err).NotTo(HaveOccurred())
				Expect(state.Result).To(Equal((i+1)*10 + 1))
			}
		})
	})
})
