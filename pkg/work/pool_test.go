package work_test

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

func poolEntry(initialState string, units ...work.WorkUnit[string, int]) work.PoolEntry[string, int] {
	return work.PoolEntry[string, int]{
		InitialState: initialState,
		Builder:      wb(units...),
	}
}

var _ = Describe("Pool", func() {
	Context("Start", func() {
		It("should run all entries to completion with correct results", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
					unit("add-100", func(_ context.Context, r int) (int, error) { return r + 100, nil }),
					unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				),
				"b": poolEntry("pending",
					unit("add-200", func(_ context.Context, r int) (int, error) { return r + 200, nil }),
					unit("add-2", func(_ context.Context, r int) (int, error) { return r + 2, nil }),
				),
			}

			pool := work.NewPool[string, int](4, entries)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			stateA, err := pool.State("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateA.Err).NotTo(HaveOccurred())
			Expect(stateA.Result).To(Equal(101))

			stateB, err := pool.State("b")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateB.Err).NotTo(HaveOccurred())
			Expect(stateB.Result).To(Equal(202))
		})

		It("should return error on double start", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
					unit("fast", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool[string, int](1, entries)
			Expect(pool.Start()).To(Succeed())

			err := pool.Start()
			Expect(err).To(MatchError("service already started"))
		})

		It("should isolate errors between entries", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"fail": poolEntry("pending",
					unit("boom", func(_ context.Context, _ int) (int, error) { return 0, errors.New("boom") }),
				),
				"ok": poolEntry("pending",
					unit("add-42", func(_ context.Context, r int) (int, error) { return r + 42, nil }),
				),
			}

			pool := work.NewPool[string, int](2, entries)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			stateFail, err := pool.State("fail")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateFail.Err).To(MatchError("boom"))

			stateOk, err := pool.State("ok")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateOk.Err).NotTo(HaveOccurred())
			Expect(stateOk.Result).To(Equal(42))
		})
	})

	Context("Cancel", func() {
		It("should cancel a single entry without affecting others", func() {
			gate := make(chan struct{})

			entries := map[string]work.PoolEntry[string, int]{
				"slow": poolEntry("pending",
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r + 1, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
				"fast": poolEntry("pending",
					unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				),
			}

			pool := work.NewPool[string, int](4, entries)
			Expect(pool.Start()).To(Succeed())

			Eventually(func() bool {
				s, _ := pool.State("fast")
				return s.Err == nil && s.Result == 10
			}).Should(BeTrue())

			pool.Cancel("slow")

			Eventually(pool.IsRunning).Should(BeFalse())

			stateSlow, err := pool.State("slow")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateSlow.Err).To(MatchError("pipeline is stopped"))

			stateFast, err := pool.State("fast")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateFast.Err).NotTo(HaveOccurred())
			Expect(stateFast.Result).To(Equal(10))
		})
	})

	Context("Stop", func() {
		It("should stop all entries and persist state", func() {
			gate := make(chan struct{})

			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
					unit("blocking", func(ctx context.Context, r int) (int, error) {
						select {
						case <-gate:
							return r, nil
						case <-ctx.Done():
							return r, ctx.Err()
						}
					}),
				),
				"b": poolEntry("pending",
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

			pool := work.NewPool[string, int](4, entries)
			Expect(pool.Start()).To(Succeed())

			pool.Stop()

			Expect(pool.IsRunning()).To(BeFalse())

			stateA, err := pool.State("a")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateA.Err).To(HaveOccurred())

			stateB, err := pool.State("b")
			Expect(err).NotTo(HaveOccurred())
			Expect(stateB.Err).To(HaveOccurred())
		})

		It("should be safe to call when not started", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
					unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool[string, int](1, entries)
			Expect(func() { pool.Stop() }).NotTo(Panic())
		})
	})

	Context("State", func() {
		It("should return error for unknown key", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
					unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
				),
			}

			pool := work.NewPool[string, int](1, entries)
			Expect(pool.Start()).To(Succeed())
			Eventually(pool.IsRunning).Should(BeFalse())

			_, err := pool.State("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown key"))
		})
	})

	Context("IsRunning", func() {
		It("should reflect running state", func() {
			gate := make(chan struct{})
			defer close(gate)

			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
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

			pool := work.NewPool[string, int](1, entries)
			Expect(pool.Start()).To(Succeed())

			Expect(pool.IsRunning()).To(BeTrue())
		})
	})

	Context("concurrent cancel safety", func() {
		It("should handle concurrent Cancel calls without races", func() {
			entries := map[string]work.PoolEntry[string, int]{
				"a": poolEntry("pending",
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

			pool := work.NewPool[string, int](1, entries)
			Expect(pool.Start()).To(Succeed())

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			for range n {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					pool.Cancel("a")
				}()
			}

			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
			Expect(pool.IsRunning()).To(BeFalse())
		})
	})
})
