package work_test

import (
	"context"
	"errors"
	"sync"
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
			p := work.NewPipeline2[string, int](nil, wb(units...))

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

			p := work.NewPipeline2(sched, wb(units...))
			_, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			_, err = p.Start()
			Expect(err).To(MatchError("pipeline is already running"))

			close(gate)
			p.Stop()
		})

		It("should close the channel when no units are provided", func() {
			p := work.NewPipeline2(sched, wb())

			c, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			Eventually(c).Should(BeClosed())
		})
	})

	Context("sequential execution", func() {
		It("should execute units in order and thread the result", func() {
			units := []work.WorkUnit[string, int]{
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("mul-2", func(_ context.Context, r int) (int, error) { return r * 2, nil }),
			}

			p := work.NewPipeline2(sched, wb(units...))
			c, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			var statuses []work.Status[string, int]
			for s := range c {
				statuses = append(statuses, s)
			}

			Expect(statuses).To(HaveLen(3))
			Expect(statuses[0].State).To(Equal("add-1"))
			Expect(statuses[0].Result).To(Equal(1))
			Expect(statuses[1].State).To(Equal("add-10"))
			Expect(statuses[1].Result).To(Equal(11))
			Expect(statuses[2].State).To(Equal("mul-2"))
			Expect(statuses[2].Result).To(Equal(22))
		})

		It("should stop on first error and report it", func() {
			expectedErr := errors.New("unit-2 failed")

			units := []work.WorkUnit[string, int]{
				unit("ok", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("fail", func(_ context.Context, _ int) (int, error) { return 0, expectedErr }),
				unit("never", func(_ context.Context, r int) (int, error) { return r, nil }),
			}

			p := work.NewPipeline2(sched, wb(units...))
			c, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			var statuses []work.Status[string, int]
			for s := range c {
				statuses = append(statuses, s)
			}

			Expect(statuses).To(HaveLen(2))
			Expect(statuses[0].State).To(Equal("ok"))
			Expect(statuses[1].Err).To(MatchError(expectedErr))
		})
	})

	Context("Stop", func() {
		It("should be safe to call when not running", func() {
			p := work.NewPipeline2(sched, wb())
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

			p := work.NewPipeline2(sched, wb(units...))
			c, err := p.Start()
			Expect(err).NotTo(HaveOccurred())

			p.Stop()

			Eventually(c).Should(BeClosed())
		})

		It("should not deadlock when stop races with natural completion", func() {
			units := []work.WorkUnit[string, int]{
				unit("fast", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
			}

			p := work.NewPipeline2(sched, wb(units...))
			c, _ := p.Start()

			// drain so pipeline can advance
			go func() {
				for range c {
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

				p := work.NewPipeline2(multiSched, wb(units...))
				c, err := p.Start()
				Expect(err).NotTo(HaveOccurred())

				go func(idx int, c chan work.Status[string, int]) {
					defer wg.Done()
					defer GinkgoRecover()
					var last work.Status[string, int]
					for s := range c {
						last = s
					}
					results[idx] = last.Result
				}(i, c)
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

			p := work.NewPipeline2(stressSched, wb(units...))
			c, _ := p.Start()
			go func() {
				for range c {
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
})
