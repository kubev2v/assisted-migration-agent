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

var _ = Describe("Pipeline2", func() {
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
		It("should return error when work builder is nil", func() {
			p := work.NewPipeline2[string, int](sched, nil)

			_, err := p.Start()

			Expect(err).To(HaveOccurred())
		})

		It("should return error when scheduler is nil", func() {
			units := []work.WorkUnit[string, int]{
				unit("step", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			p := work.NewPipeline2[string, int](nil, newTestBuilder(nil, units...))

			_, err := p.Start()

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

			p := work.NewPipeline2(sched, newTestBuilder(nil, units...))
			_, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			_, err = p.Start()
			Expect(err).To(MatchError("pipeline is already running"))

			close(gate)
			p.Stop()
		})

		It("should close the channel when no units are provided", func() {
			p := work.NewPipeline2(sched, newTestBuilder(nil))

			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			Eventually(ticks).Should(BeClosed())
		})
	})

	Context("sequential execution", func() {
		It("should execute units in order and thread the result", func() {
			units := []work.WorkUnit[string, int]{
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("mul-2", func(_ context.Context, r int) (int, error) { return r * 2, nil }),
			}

			p := work.NewPipeline2(sched, newTestBuilder(nil, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			var count int
			for range ticks {
				count++
			}

			Expect(count).To(Equal(3))

			result, pErr := p.Result()
			Expect(pErr).NotTo(HaveOccurred())
			Expect(result).To(Equal(22))
		})

		It("should stop on first error and report it with extra error tick", func() {
			expectedErr := errors.New("unit-2 failed")

			units := []work.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("fail", func(_ context.Context, _ int) (int, error) { return 0, expectedErr }),
				unit("never", func(_ context.Context, r int) (int, error) { return r, nil }),
			}

			p := work.NewPipeline2(sched, newTestBuilder(nil, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			var count int
			for range ticks {
				count++
			}

			Expect(count).To(Equal(3), "expected 2 normal ticks + 1 error tick")
			_, pErr := p.Result()
			Expect(pErr).To(MatchError(expectedErr))
		})
	})

	Context("Stop", func() {
		It("should be safe to call when not running", func() {
			p := work.NewPipeline2(sched, newTestBuilder(nil))
			Expect(func() { p.Stop() }).NotTo(Panic())
		})

		It("should cancel a running pipeline and close the channel", func() {
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

			p := work.NewPipeline2(sched, newTestBuilder(nil, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			<-ticks
			p.Stop()

			Eventually(ticks).Should(BeClosed())
			Expect(p.State()).To(Equal("blocking"))
		})

		It("should not deadlock when stop races with natural completion", func() {
			units := []work.WorkUnit[string, int]{
				unit("fast", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := work.NewPipeline2(sched, newTestBuilder(nil, units...))
			ticks, _ := p.Start()

			go func() {
				for range ticks {
				}
			}()

			time.Sleep(5 * time.Millisecond)

			stopDone := make(chan struct{})
			go func() {
				p.Stop()
				close(stopDone)
			}()

			Eventually(stopDone, 2*time.Second).Should(BeClosed())
		})
	})

	Context("multiple pipelines on the same scheduler", func() {
		It("should run two pipelines concurrently", func() {
			multiSched := newScheduler(4, 0)
			defer multiSched.Close()

			var wg sync.WaitGroup
			results := make([]int, 2)

			for i := range 2 {
				wg.Add(1)
				offset := (i + 1) * 100
				units := []work.WorkUnit[string, int]{
					unit("step-a", func(_ context.Context, r int) (int, error) { return r + offset, nil }),
					unit("step-b", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				}

				p := work.NewPipeline2(multiSched, newTestBuilder(nil, units...))
				ticks, err := p.Start()
				Expect(err).NotTo(HaveOccurred())

				go func(idx int, ticks chan struct{}, p *work.Pipeline2[string, int]) {
					defer wg.Done()
					defer GinkgoRecover()
					for range ticks {
					}
					result, _ := p.Result()
					results[idx] = result
				}(i, ticks, p)
			}

			wg.Wait()
			Expect(results[0]).To(Equal(101))
			Expect(results[1]).To(Equal(201))
		})
	})

	Context("stress", func() {
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

			p := work.NewPipeline2(stressSched, newTestBuilder(nil, units...))
			ticks, _ := p.Start()
			go func() {
				for range ticks {
				}
			}()

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
		})
	})

	Context("Finalize", func() {
		It("should call Finalize with the accumulated result", func() {
			finalizeSched := newScheduler(1, 1)
			defer finalizeSched.Close()

			var receivedResult atomic.Int64
			units := []work.WorkUnit[string, int]{
				unit("add-100", func(_ context.Context, r int) (int, error) { return r + 100, nil }),
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := work.NewPipeline2(finalizeSched, newTestBuilder(
				func(_ context.Context, result int) error {
					receivedResult.Store(int64(result))
					return nil
				}, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			for range ticks {
			}

			Expect(receivedResult.Load()).To(Equal(int64(101)))
		})

		It("should surface Finalize error via Result", func() {
			finalizeSched := newScheduler(1, 1)
			defer finalizeSched.Close()

			units := []work.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := work.NewPipeline2(finalizeSched, newTestBuilder(
				func(_ context.Context, _ int) error {
					return errors.New("finalize boom")
				}, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			for range ticks {
			}

			_, pErr := p.Result()
			Expect(pErr).To(MatchError("finalize boom"))
		})

		It("should run Finalize when stopped mid-pipeline", func() {
			finalizeSched := newScheduler(1, 1)
			defer finalizeSched.Close()

			var finalized atomic.Bool
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
			}

			p := work.NewPipeline2(finalizeSched, newTestBuilder(
				func(_ context.Context, _ int) error {
					finalized.Store(true)
					return nil
				}, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			<-ticks
			p.Stop()

			Eventually(ticks).Should(BeClosed())
			Expect(finalized.Load()).To(BeTrue())
		})

		It("should run Finalize when a work unit errors", func() {
			finalizeSched := newScheduler(1, 1)
			defer finalizeSched.Close()

			var finalized atomic.Bool
			units := []work.WorkUnit[string, int]{
				unit("boom", func(_ context.Context, _ int) (int, error) {
					return 0, errors.New("work failed")
				}),
			}

			p := work.NewPipeline2(finalizeSched, newTestBuilder(
				func(_ context.Context, _ int) error {
					finalized.Store(true)
					return nil
				}, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			for range ticks {
			}

			Expect(finalized.Load()).To(BeTrue())
			_, pErr := p.Result()
			Expect(pErr).To(MatchError("work failed"))
		})
	})

	Context("State", func() {
		It("should reflect state transitions at tick boundaries across multiple units", func() {
			finalizeSched := newScheduler(1, 1)
			defer finalizeSched.Close()

			gates := [3]chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			names := [3]string{"step-1", "step-2", "step-3"}

			units := make([]work.WorkUnit[string, int], 3)
			for i := range 3 {
				gate := gates[i]
				offset := (i + 1) * 10
				units[i] = unit(names[i], func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r + offset, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				})
			}

			p := work.NewPipeline2(finalizeSched, newTestBuilder(nil, units...))
			ticks, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			for i := range 3 {
				<-ticks
				Expect(p.State()).To(Equal(names[i]))
				close(gates[i])
			}

			for range ticks {
			}

			result, pErr := p.Result()
			Expect(pErr).NotTo(HaveOccurred())
			Expect(result).To(Equal(60))
		})
	})
})
