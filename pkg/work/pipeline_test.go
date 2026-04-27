package work_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

func unit(status string, fn func(ctx context.Context, r int) (int, error)) work.WorkUnit[string, int] {
	return work.WorkUnit[string, int]{
		Status: func() string { return status },
		Work:   fn,
	}
}

func wb(units ...work.WorkUnit[string, int]) work.WorkBuilder[string, int] {
	return work.NewSliceWorkBuilder(units)
}

var _ = Describe("Pipeline", func() {
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
		It("should be a no-op for empty units", func() {
			p := work.NewPipeline[string, int]("pending", sched, nil)

			err := p.Start()

			Expect(err).NotTo(HaveOccurred())
			Expect(p.IsRunning()).To(BeFalse())
			state := p.State()
			Expect(state.State).To(Equal("pending"))
			Expect(state.Result).To(Equal(0))
			Expect(state.Err).NotTo(HaveOccurred())
		})

		It("should return error when scheduler is nil", func() {
			units := []work.WorkUnit[string, int]{
				unit("step", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			p := work.NewPipeline[string, int]("pending", nil, wb(units...))

			err := p.Start()

			Expect(err).To(HaveOccurred())
		})

		It("should return error on double start", func() {
			gate := make(chan struct{})
			units := []work.WorkUnit[string, int]{
				unit("slow", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
					case <-ctx.Done():
					}
					return r, ctx.Err()
				}),
			}

			p := work.NewPipeline("pending", sched, wb(units...))
			Expect(p.Start()).To(Succeed())

			err := p.Start()

			Expect(err).To(MatchError("pipeline is already running"))

			close(gate)
			Eventually(p.IsRunning).Should(BeFalse())
		})
	})

	Context("sequential execution", func() {
		It("should execute units in order and thread the result", func() {
			units := []work.WorkUnit[string, int]{
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("mul-2", func(_ context.Context, r int) (int, error) { return r * 2, nil }),
			}

			p := work.NewPipeline("pending", sched, wb(units...))

			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			state := p.State()
			Expect(state.Err).NotTo(HaveOccurred())
			Expect(state.Result).To(Equal((0 + 1 + 10) * 2))
		})

		It("should expose current state while running", func() {
			gate := make(chan struct{})

			units := []work.WorkUnit[string, int]{
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

			p := work.NewPipeline("pending", sched, wb(units...))

			Expect(p.Start()).To(Succeed())
			Eventually(func() string {
				return p.State().State
			}).Should(Equal("first"))
			Expect(p.State().Result).To(Equal(0))
			close(gate)
			Eventually(p.IsRunning).Should(BeFalse())

			state := p.State()
			Expect(state.State).To(Equal("second"))
			Expect(state.Result).To(Equal(2))
			Expect(state.Err).NotTo(HaveOccurred())
		})

		It("should stop on first error and report it via State", func() {
			expectedErr := errors.New("unit-2 failed")
			var callCount atomic.Int32

			units := []work.WorkUnit[string, int]{
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

			p := work.NewPipeline("pending", sched, wb(units...))

			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			Expect(p.State().Err).To(MatchError(expectedErr))
			Expect(callCount.Load()).To(Equal(int32(2)))
		})

		It("should expose terminal error through State", func() {
			units := []work.WorkUnit[string, int]{
				{
					Status: func() string { return "failing" },
					Work:   func(_ context.Context, r int) (int, error) { return r, errors.New("boom") },
				},
			}

			p := work.NewPipeline("pending", sched, wb(units...))

			Expect(p.Start()).To(Succeed())
			Eventually(p.IsRunning).Should(BeFalse())

			state := p.State()
			Expect(state.State).To(Equal("failing"))
			Expect(state.Err).To(MatchError("boom"))
		})
	})

	Context("Stop", func() {
		It("should be safe to call when not running", func() {
			units := []work.WorkUnit[string, int]{
				unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			p := work.NewPipeline("pending", sched, wb(units...))

			Expect(func() { p.Stop() }).NotTo(Panic())
		})

		It("should cancel a running pipeline", func() {
			gate := make(chan struct{})
			units := []work.WorkUnit[string, int]{
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

			p := work.NewPipeline("pending", sched, wb(units...))
			Expect(p.Start()).To(Succeed())
			Expect(p.IsRunning()).To(BeTrue())

			p.Stop()

			Expect(p.State().Err).To(MatchError("pipeline is stopped"))
			Expect(p.IsRunning()).To(BeFalse())
		})

		It("should allow restart after stop", func() {
			gate := make(chan struct{})
			makeUnits := func() []work.WorkUnit[string, int] {
				return []work.WorkUnit[string, int]{
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

			p1 := work.NewPipeline("pending", sched, wb(makeUnits()...))
			Expect(p1.Start()).To(Succeed())
			p1.Stop()
			Expect(p1.IsRunning()).To(BeFalse())

			p2 := work.NewPipeline("pending", sched, wb(makeUnits()...))

			Expect(p2.Start()).To(Succeed())
			close(gate)

			Eventually(p2.IsRunning).Should(BeFalse())
			Expect(p2.State().Err).NotTo(HaveOccurred())
			Expect(p2.State().Result).To(Equal(1))
		})

		It("should not deadlock when stop races with natural completion", func() {
			units := []work.WorkUnit[string, int]{
				unit("fast", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := work.NewPipeline("pending", sched, wb(units...))
			Expect(p.Start()).To(Succeed())
			time.Sleep(5 * time.Millisecond)

			stopDone := make(chan struct{})
			go func() {
				p.Stop()
				close(stopDone)
			}()

			Eventually(stopDone, 2*time.Second).Should(BeClosed())
			Expect(p.IsRunning()).To(BeFalse())
		})
	})

	Context("multiple pipelines on the same scheduler", func() {
		It("should run two pipelines concurrently on a multi-worker scheduler", func() {
			multiSched := newScheduler(4, 0)
			defer multiSched.Close()

			pipelines := make([]*work.Pipeline[string, int], 2)

			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				offset := (i + 1) * 100

				units := []work.WorkUnit[string, int]{
					unit("step-a", func(_ context.Context, r int) (int, error) { return r + offset, nil }),
					unit("step-b", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				}

				pipelines[i] = work.NewPipeline("pending", multiSched, wb(units...))

				go func(p *work.Pipeline[string, int]) {
					defer wg.Done()
					Expect(p.Start()).To(Succeed())
				}(pipelines[i])
			}

			wg.Wait()
			for i, p := range pipelines {
				Eventually(p.IsRunning).Should(BeFalse())
				state := p.State()
				Expect(state.Err).NotTo(HaveOccurred())
				Expect(state.Result).To(Equal((i+1)*100 + 1))
			}
		})

		It("should allow stopping one pipeline without affecting the other", func() {
			multiSched := newScheduler(4, 0)
			defer multiSched.Close()

			gate := make(chan struct{})

			slowUnits := []work.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r + 1, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
			}

			fastUnits := []work.WorkUnit[string, int]{
				unit("fast-a", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("fast-b", func(_ context.Context, r int) (int, error) { return r + 20, nil }),
			}

			pSlow := work.NewPipeline("pending", multiSched, wb(slowUnits...))
			pFast := work.NewPipeline("pending", multiSched, wb(fastUnits...))

			Expect(pSlow.Start()).To(Succeed())
			Expect(pFast.Start()).To(Succeed())

			Eventually(pFast.IsRunning).Should(BeFalse())
			pSlow.Stop()

			Expect(pFast.State().Err).NotTo(HaveOccurred())
			Expect(pFast.State().Result).To(Equal(30))
			Expect(pSlow.State().Err).To(MatchError("pipeline is stopped"))
		})

		It("should isolate errors between pipelines on the same scheduler", func() {
			multiSched := newScheduler(2, 0)
			defer multiSched.Close()

			failUnits := []work.WorkUnit[string, int]{
				unit("fail", func(_ context.Context, _ int) (int, error) {
					return 0, errors.New("boom")
				}),
			}

			okUnits := []work.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) { return r + 42, nil }),
			}

			pFail := work.NewPipeline("pending", multiSched, wb(failUnits...))
			pOk := work.NewPipeline("pending", multiSched, wb(okUnits...))

			Expect(pFail.Start()).To(Succeed())
			Expect(pOk.Start()).To(Succeed())

			Eventually(pFail.IsRunning).Should(BeFalse())
			Eventually(pOk.IsRunning).Should(BeFalse())

			Expect(pFail.State().Err).To(MatchError("boom"))
			Expect(pOk.State().Err).NotTo(HaveOccurred())
			Expect(pOk.State().Result).To(Equal(42))
		})
	})

	Context("stress", func() {
		It("should handle start + immediate stop without races", func() {
			stressSched := newScheduler(4, 0)
			defer stressSched.Close()

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			for i := range n {
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()

					units := []work.WorkUnit[string, int]{
						unit("blocking", func(ctx context.Context, r int) (int, error) {
							select {
							case <-ctx.Done():
								return r, ctx.Err()
							case <-time.After(time.Second):
								return r + idx, nil
							}
						}),
					}

					p := work.NewPipeline("pending", stressSched, wb(units...))

					Expect(p.Start()).To(Succeed())
					p.Stop()

					Expect(p.IsRunning()).To(BeFalse())
				}(i)
			}

			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
		})

		It("should handle concurrent Stop calls without races", func() {
			stressSched := newScheduler(1, 0)
			defer stressSched.Close()

			units := []work.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-ctx.Done():
						return r, ctx.Err()
					case <-time.After(5 * time.Second):
						return r, nil
					}
				}),
			}

			p := work.NewPipeline("pending", stressSched, wb(units...))
			Expect(p.Start()).To(Succeed())

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			for range n {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					p.Stop()
				}()
			}

			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
			Expect(p.IsRunning()).To(BeFalse())
		})

		It("should run 10 independent pipelines concurrently without races", func() {
			stressSched := newScheduler(4, 0)
			defer stressSched.Close()

			const n = 10
			pipelines := make([]*work.Pipeline[string, int], n)

			var wg sync.WaitGroup
			wg.Add(n)

			for i := range n {
				go func(idx int) {
					defer wg.Done()
					defer GinkgoRecover()

					offset := (idx + 1) * 10
					units := []work.WorkUnit[string, int]{
						unit("step-a", func(_ context.Context, r int) (int, error) { return r + offset, nil }),
						unit("step-b", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
					}

					pipelines[idx] = work.NewPipeline("pending", stressSched, wb(units...))
					Expect(pipelines[idx].Start()).To(Succeed())
				}(i)
			}

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
