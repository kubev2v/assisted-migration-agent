package work_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type testBuilder struct {
	units      []work.WorkUnit[string, int]
	idx        int
	finalizeFn func(ctx context.Context, result int) error
}

func (b *testBuilder) Next() (work.WorkUnit[string, int], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[string, int]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *testBuilder) Finalize(ctx context.Context, result int) error {
	if b.finalizeFn != nil {
		return b.finalizeFn(ctx, result)
	}
	return nil
}

func newTestBuilder(finalizeFn func(ctx context.Context, result int) error, units ...work.WorkUnit[string, int]) work.WorkBuilder2[string, int] {
	return &testBuilder{units: units, finalizeFn: finalizeFn}
}

var _ = Describe("Pool2", func() {
	Context("Start", func() {
		It("should run all entries to completion with correct results", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("add-100", func(_ context.Context, r int) (int, error) { return r + 100, nil }),
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
				"b": newTestBuilder(nil,
					unit("add-200", func(_ context.Context, r int) (int, error) { return r + 200, nil }),
					unit("add-2", func(_ context.Context, r int) (int, error) { return r + 2, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			resultA, err := pool.Result("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(resultA).To(Equal(101))

			resultB, err := pool.Result("b")
			Expect(err).NotTo(HaveOccurred())
			Expect(resultB).To(Equal(202))
		})

		It("should return error on double start", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("fast", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			defer func() { _ = pool.Stop() }()

			err := pool.Start()
			Expect(err).To(MatchError("service already started"))
		})

		It("should isolate errors between entries", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"fail": newTestBuilder(nil,
					unit("boom", func(_ context.Context, _ int) (int, error) { return 0, errors.New("boom") }),
				),
				"ok": newTestBuilder(nil,
					unit("add-42", func(_ context.Context, r int) (int, error) { return r + 42, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, errFail := pool.Result("fail")
			Expect(errFail).To(MatchError("boom"))

			resultOk, errOk := pool.Result("ok")
			Expect(errOk).NotTo(HaveOccurred())
			Expect(resultOk).To(Equal(42))
		})
	})

	Context("Cancel", func() {
		It("should cancel a single entry without affecting others", func() {
			gate := make(chan struct{})

			builders := map[string]work.WorkBuilder2[string, int]{
				"slow": newTestBuilder(nil,
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r + 1, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
				"fast": newTestBuilder(nil,
					unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() int {
				r, _ := pool.Result("fast")
				return r
			}).Should(Equal(10))

			_, err := pool.Cancel("slow")
			Expect(err).NotTo(HaveOccurred())

			resultSlow, _ := pool.Result("slow")
			Expect(resultSlow).To(Equal(0))
			Eventually(pool.IsRunning).Should(BeFalse())

			resultFast, errFast := pool.Result("fast")
			Expect(errFast).NotTo(HaveOccurred())
			Expect(resultFast).To(Equal(10))
		})

		It("should return the final status including finalize error", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, _ int) error { return errors.New("finalize failed") },
					unit("step-1", func(_ context.Context, r int) (int, error) { return r + 5, nil }),
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						<-ctx.Done()
						return r, ctx.Err()
					}),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() string {
				s, _ := pool.State("a")
				return s
			}).Should(Equal("blocking"))

			_, err := pool.Cancel("a")
			Expect(err).NotTo(HaveOccurred())

			_, resultErr := pool.Result("a")
			Expect(resultErr).To(MatchError("finalize failed"))
		})

		It("should return error for unknown key", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, err := pool.Cancel("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown key"))
		})

		It("should return state for already-done pipeline without calling Stop", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			state, err := pool.Cancel("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(state).To(Equal("add-1"))
		})
	})

	Context("Stop", func() {
		It("should block until all pipelines and finalize complete", func() {
			finalizeGate := make(chan struct{})
			var finalizeCalled atomic.Bool

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("fast", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders).
				WithFinalizer(func(_ context.Context) error {
					finalizeCalled.Store(true)
					<-finalizeGate
					return nil
				})
			Expect(pool.Start()).To(Succeed())

			Eventually(finalizeCalled.Load).Should(BeTrue())

			stopped := make(chan struct{})
			go func() {
				Expect(pool.Stop()).To(BeNil())
				close(stopped)
			}()

			Consistently(stopped, 50*time.Millisecond).ShouldNot(BeClosed())

			close(finalizeGate)
			Eventually(stopped, 2*time.Second).Should(BeClosed())
			Expect(pool.IsRunning()).To(BeFalse())
		})

		It("should not block when called twice", func() {
			gate := make(chan struct{})

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r + 1, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			Expect(pool.Stop()).To(BeNil())

			stopped := make(chan struct{})
			go func() {
				Expect(pool.Stop()).To(BeNil())
				close(stopped)
			}()
			Eventually(stopped, 2*time.Second).Should(BeClosed())
		})
	})

	Context("State", func() {
		It("should return error for unknown key", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, err := pool.State("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown key"))
		})

		It("should reflect status updates from the pipeline", func() {
			gate := make(chan struct{})

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("step-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
					unit("step-2", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r + 10, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() string {
				s, _ := pool.State("a")
				return s
			}).Should(Equal("step-2"))

			close(gate)
			Eventually(pool.IsRunning).Should(BeFalse())

			result, err := pool.Result("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(11))
		})
	})

	Context("Result", func() {
		It("should return error for unknown key", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, err := pool.Result("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown key"))
		})
	})

	Context("per-pipeline finalize", func() {
		It("should run finalize for each pipeline after it completes", func() {
			var finalizedA, finalizedB atomic.Bool

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, _ int) error { finalizedA.Store(true); return nil },
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
				"b": newTestBuilder(
					func(_ context.Context, _ int) error { finalizedB.Store(true); return nil },
					unit("add-2", func(_ context.Context, r int) (int, error) { return r + 2, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(finalizedA.Load()).To(BeTrue())
			Expect(finalizedB.Load()).To(BeTrue())
		})

		It("should run finalize even when the pipeline errors", func() {
			var finalized atomic.Bool

			builders := map[string]work.WorkBuilder2[string, int]{
				"fail": newTestBuilder(
					func(_ context.Context, _ int) error { finalized.Store(true); return nil },
					unit("boom", func(_ context.Context, _ int) (int, error) { return 0, errors.New("boom") }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(finalized.Load()).To(BeTrue())
		})

		It("should run finalize even when the pipeline is cancelled", func() {
			var finalized atomic.Bool
			gate := make(chan struct{})

			builders := map[string]work.WorkBuilder2[string, int]{
				"slow": newTestBuilder(
					func(_ context.Context, _ int) error { finalized.Store(true); return nil },
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			_, err := pool.Cancel("slow")
			Expect(err).NotTo(HaveOccurred())

			resultSlow, _ := pool.Result("slow")
			Expect(resultSlow).To(Equal(0))
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(finalized.Load()).To(BeTrue())
		})

		It("should handle nil finalize gracefully", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			result, err := pool.Result("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(1))
		})

		It("should pass the final result to finalize", func() {
			var receivedResult atomic.Int64

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, result int) error {
						receivedResult.Store(int64(result))
						return nil
					},
					unit("add-100", func(_ context.Context, r int) (int, error) { return r + 100, nil }),
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(receivedResult.Load()).To(Equal(int64(101)))
		})

		It("should surface finalize error via Result", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, _ int) error { return errors.New("finalize failed") },
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, err := pool.Result("a")
			Expect(err).To(MatchError("finalize failed"))
		})
	})

	Context("pool-level finalize", func() {
		It("should run pool finalize after all pipelines complete", func() {
			var order []string
			var mu sync.Mutex
			append := func(s string) {
				mu.Lock()
				order = append(order, s)
				mu.Unlock()
			}

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, _ int) error { append("finalize-a"); return nil },
					unit("work-a", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders).
				WithFinalizer(func(_ context.Context) error {
					append("general")
					return nil
				})
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(pool.Stop()).To(BeNil())

			mu.Lock()
			defer mu.Unlock()
			Expect(order).To(HaveLen(2))
			Expect(order[0]).To(Equal("finalize-a"))
			Expect(order[1]).To(Equal("general"))
		})

		It("should run pool finalize even when a pipeline errors", func() {
			var generalCalled atomic.Bool

			builders := map[string]work.WorkBuilder2[string, int]{
				"fail": newTestBuilder(nil,
					unit("boom", func(_ context.Context, _ int) (int, error) { return 0, errors.New("boom") }),
				),
			}

			pool := work.NewPool2[string, int](builders).
				WithFinalizer(func(_ context.Context) error {
					generalCalled.Store(true)
					return nil
				})
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			Expect(pool.Stop()).To(BeNil())

			Expect(generalCalled.Load()).To(BeTrue())

			_, errFail := pool.Result("fail")
			Expect(errFail).To(MatchError("boom"))
		})

		It("should not run pool finalize if not set", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			Expect(pool.Stop()).To(BeNil())

			Expect(pool.IsRunning()).To(BeFalse())
		})

		It("should run per-pipeline finalize before pool finalize", func() {
			var order []string
			var mu sync.Mutex
			append := func(s string) {
				mu.Lock()
				order = append(order, s)
				mu.Unlock()
			}

			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(
					func(_ context.Context, _ int) error { append("pipeline-a"); return nil },
					unit("work", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
				"b": newTestBuilder(
					func(_ context.Context, _ int) error { append("pipeline-b"); return nil },
					unit("work", func(_ context.Context, r int) (int, error) { return r + 2, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders).
				WithFinalizer(func(_ context.Context) error {
					append("general")
					return nil
				})
			Expect(pool.Start()).To(Succeed())

			Expect(pool.Stop()).To(BeNil())

			mu.Lock()
			defer mu.Unlock()
			Expect(order).To(HaveLen(3))
			Expect(order[2]).To(Equal("general"))
		})

		It("should return pool finalize error from Stop", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
			}

			pool := work.NewPool2[string, int](builders).
				WithFinalizer(func(_ context.Context) error {
					return errors.New("pool finalize failed")
				})
			Expect(pool.Start()).To(Succeed())

			err := pool.Stop()
			Expect(err).To(MatchError("pool finalize failed"))
		})
	})

	Context("concurrent cancel safety", func() {
		It("should handle concurrent Cancel calls without races", func() {
			builders := map[string]work.WorkBuilder2[string, int]{
				"a": newTestBuilder(nil,
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-ctx.Done():
							return r, ctx.Err()
						case <-time.After(5 * time.Second):
							return r, nil
						}
					}),
				),
			}

			pool := work.NewPool2[string, int](builders)
			Expect(pool.Start()).To(Succeed())

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			for range n {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					_, _ = pool.Cancel("a")
				}()
			}

			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
			Eventually(pool.IsRunning).Should(BeFalse())
		})
	})
})
