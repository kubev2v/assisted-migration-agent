package services

import (
	"context"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
)

var _ = Describe("inspectionService", func() {
	Describe("GetVmStatus", func() {
		It("returns NotFound when no pipelines exist", func() {
			svc := newInspectionService(nil)
			status := svc.GetVmStatus("vm-1")
			Expect(status.State).To(Equal(models.InspectionStateNotStarted))
		})

		It("returns pipeline state after Start", func() {
			svc := newInspectionService(nil).WithWorkUnitsBuilder(func(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult] {
				return []models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateRunning}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							return result, nil
						},
					},
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateCompleted}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							return result, nil
						},
					},
				}
			})

			err := svc.Start(nil, nil, []string{"vm-1"})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() models.InspectionState {
				status := svc.GetVmStatus("vm-1")
				return status.State
			}).Should(Equal(models.InspectionStateCompleted))
		})
	})

	Describe("CancelVmInspection", func() {
		It("stops specified pipelines", func() {
			var block sync.WaitGroup
			block.Add(1)
			svc := newInspectionService(nil).WithWorkUnitsBuilder(func(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult] {
				return []models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateRunning}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							block.Wait()
							return result, nil
						},
					},
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateCompleted}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							return result, nil
						},
					},
				}
			})

			err := svc.Start(nil, nil, []string{"vm-1", "vm-2"})
			Expect(err).NotTo(HaveOccurred())

			svc.CancelVmInspection("vm-1")

			block.Done()

			Eventually(func() models.InspectionState {
				s := svc.GetVmStatus("vm-1")
				return s.State
			}).Should(Equal(models.InspectionStateCanceled))

			// vm-2 should still complete
			Eventually(func() models.InspectionState {
				s := svc.GetVmStatus("vm-2")
				return s.State
			}).Should(Equal(models.InspectionStateCompleted))
		})
	})

	Describe("Start", func() {
		It("stores operator and creates pipelines for given IDs", func() {
			svc := newInspectionService(nil).WithWorkUnitsBuilder(func(id string) []models.WorkUnit[models.InspectionStatus, models.InspectionResult] {
				return []models.WorkUnit[models.InspectionStatus, models.InspectionResult]{
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateRunning}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							return result, nil
						},
					},
					{
						Status: func() models.InspectionStatus {
							return models.InspectionStatus{State: models.InspectionStateCompleted}
						},
						Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
							return result, nil
						},
					},
				}
			})

			err := svc.Start((*vmware.VMManager)(nil), nil, []string{"vm-a", "vm-b"})
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.operator).To(BeNil())

			Eventually(func() models.InspectionState {
				s := svc.GetVmStatus("vm-a")
				return s.State
			}).Should(Equal(models.InspectionStateCompleted))
			Eventually(func() models.InspectionState {
				s := svc.GetVmStatus("vm-b")
				return s.State
			}).Should(Equal(models.InspectionStateCompleted))
		})
	})
})
