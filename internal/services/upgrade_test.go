package services_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type finishingCollector struct {
	started    bool
	finalizing chan struct{}
	gate       chan struct{}
}

func (b *finishingCollector) Build() work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return b
}
func (b *finishingCollector) Next() (work.WorkUnit[models.CollectorStatus, models.CollectorResult], bool) {
	if b.started {
		return work.WorkUnit[models.CollectorStatus, models.CollectorResult]{}, false
	}
	b.started = true
	return work.WorkUnit[models.CollectorStatus, models.CollectorResult]{Status: func() models.CollectorStatus { return models.CollectorStatus{State: models.CollectorStateCollected} }, Work: func(_ context.Context, _ models.CollectorResult) (models.CollectorResult, error) {
		return models.CollectorResult{Completed: true}, nil
	}}, true
}
func (b *finishingCollector) Finalize(ctx context.Context, _ models.CollectorResult) error {
	close(b.finalizing)
	select {
	case <-b.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ = Describe("Upgrade work exclusion", func() {
	It("waits for collector finalization even after the UI state is terminal", func() {
		builder := &finishingCollector{finalizing: make(chan struct{}), gate: make(chan struct{})}
		manager := services.NewServiceManager(GinkgoT().TempDir(), services.WithCollectorWorkBuilder(builder))
		DeferCleanup(func() { manager.Stop(context.Background()) })
		_, err := manager.StartCollecting(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Eventually(builder.finalizing).Should(BeClosed())
		Expect(manager.GetCollectorStatus().State).To(Equal(models.CollectorStateCollected))
		Expect(manager.UpgradeBusy()).To(BeTrue())
		close(builder.gate)
		Eventually(manager.UpgradeBusy, time.Second).Should(BeFalse())
	})
})
