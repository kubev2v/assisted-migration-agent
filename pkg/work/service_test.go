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

var _ = Describe("Service", func() {
	Context("Start", func() {
		It("should run to completion and thread the result", func() {
			units := []work.WorkUnit[string, int]{
				unit("add-1", func(_ context.Context, r int) (int, error) { return r + 1, nil }),
				unit("add-10", func(_ context.Context, r int) (int, error) { return r + 10, nil }),
				unit("mul-2", func(_ context.Context, r int) (int, error) { return r * 2, nil }),
			}

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())

			Eventually(srv.IsRunning).Should(BeFalse())

			state := srv.State()
			Expect(state.Err).NotTo(HaveOccurred())
			Expect(state.Result).To(Equal((0 + 1 + 10) * 2))
		})

		It("should return error on double start", func() {
			gate := make(chan struct{})
			defer close(gate)

			units := []work.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
			}

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())

			err := srv.Start()
			Expect(err).To(MatchError("service already started"))
		})
	})

	Context("Stop", func() {
		It("should be safe to call when not started", func() {
			units := []work.WorkUnit[string, int]{
				unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			srv := work.NewService("pending", wb(units...))

			Expect(func() { srv.Stop() }).NotTo(Panic())
		})

		It("should cancel a running service", func() {
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

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())
			Expect(srv.IsRunning()).To(BeTrue())

			srv.Stop()

			Expect(srv.IsRunning()).To(BeFalse())
			Expect(srv.State().Err).To(MatchError("pipeline is stopped"))
		})
	})

	Context("State", func() {
		It("should return initial state before start", func() {
			units := []work.WorkUnit[string, int]{
				unit("s", func(_ context.Context, r int) (int, error) { return r, nil }),
			}
			srv := work.NewService("pending", wb(units...))

			state := srv.State()
			Expect(state.State).To(Equal("pending"))
			Expect(state.Result).To(Equal(0))
			Expect(state.Err).NotTo(HaveOccurred())
		})

		It("should persist result after completion", func() {
			units := []work.WorkUnit[string, int]{
				unit("done", func(_ context.Context, r int) (int, error) { return r + 42, nil }),
			}

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())
			Eventually(srv.IsRunning).Should(BeFalse())

			state := srv.State()
			Expect(state.Err).NotTo(HaveOccurred())
			Expect(state.Result).To(Equal(42))
		})

		It("should persist error after failure", func() {
			units := []work.WorkUnit[string, int]{
				unit("fail", func(_ context.Context, r int) (int, error) {
					return r, errors.New("boom")
				}),
			}

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())
			Eventually(srv.IsRunning).Should(BeFalse())

			state := srv.State()
			Expect(state.Err).To(MatchError("boom"))
		})

		It("should persist error after stop", func() {
			gate := make(chan struct{})
			units := []work.WorkUnit[string, int]{
				unit("blocking", func(ctx context.Context, r int) (int, error) {
					select {
					case <-gate:
						return r, nil
					case <-ctx.Done():
						return r, ctx.Err()
					}
				}),
			}

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())
			srv.Stop()

			state := srv.State()
			Expect(state.Err).To(HaveOccurred())
		})
	})

	Context("concurrent stop safety", func() {
		It("should handle concurrent Stop calls without races", func() {
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

			srv := work.NewService("pending", wb(units...))
			Expect(srv.Start()).To(Succeed())

			const n = 10
			var wg sync.WaitGroup
			wg.Add(n)

			for range n {
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					srv.Stop()
				}()
			}

			waitCh := make(chan struct{})
			go func() {
				wg.Wait()
				close(waitCh)
			}()
			Eventually(waitCh, 10*time.Second).Should(BeClosed())
			Expect(srv.IsRunning()).To(BeFalse())
		})
	})
})
