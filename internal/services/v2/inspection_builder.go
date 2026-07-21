package v2

import (
	"context"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type inspectionBuilderFactory = func(id string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]

func defaultInspectionBuilderFactory(store *store.Store2, operator vmware.VMOperator, detector *vmdetect.Detector) inspectionBuilderFactory {
	return func(vmID string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
		log := zap.S().Named("inspection_builder")

		units := []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
			{
				Status: func() models.InspectionStatus {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating credentials"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					return status
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					log.Infow("validating VM privileges", "vmId", vmID)
					if err := operator.ValidatePrivileges(ctx, vmID, models.InspectorRequiredPrivileges); err != nil {
						log.Errorw("privilege validation failed", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("privilege validation passed", "vmId", vmID)
					return result, nil
				},
			},
			{
				Status: func() models.InspectionStatus {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating snapshot"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					return status
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					log.Infow("creating VM snapshot", "vmId", vmID)
					snapID, err := operator.CreateSnapshot(ctx, vmware.CreateSnapshotRequest{
						VmId:         vmID,
						SnapshotName: models.InspectionSnapshotName,
					})
					if err != nil {
						log.Errorw("failed to create VM snapshot", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					result.SnapshotID = snapID
					log.Infow("VM snapshot created", "vmId", vmID)
					return result, nil
				},
			},
			{
				Status: func() models.InspectionStatus {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "running deep inspection"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					return status
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					log.Infow("running deep inspection", "vmId", vmID, "snapshotId", result.SnapshotID)
					detectResult, err := detector.Detect(vmdetect.DetectParams{
						Ctx:           ctx,
						VMMoref:       vmID,
						SnapshotMoref: result.SnapshotID,
					})
					if err != nil {
						log.Errorw("deep inspection failed", "vmId", vmID, "snapshotId", result.SnapshotID, "error", err)
						result.Err = err
						return result, err
					}

					if detectResult.AllConcerns != nil {
						concerns := make([]models.VmInspectionConcern, 0, len(detectResult.AllConcerns))
						for _, c := range detectResult.AllConcerns {
							concerns = append(concerns, models.VmInspectionConcern{
								Label:    c.Label,
								Category: string(c.Category),
								Msg:      c.Message,
							})
						}
						result.Concerns = concerns
					}

					log.Infow("deep inspection completed", "vmId", vmID, "concernCount", len(result.Concerns))
					return result, nil
				},
			},
			{
				Status: func() models.InspectionStatus {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting results"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					return status
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					log.Infow("persisting inspection results", "vmId", vmID, "concernCount", len(result.Concerns))
					err := store.WithTx(ctx, func(txCtx context.Context) error {
						return store.Inspection().InsertResult(txCtx, vmID, result.Concerns)
					})
					if err != nil {
						log.Errorw("failed to persist inspection results", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("inspection results persisted", "vmId", vmID)
					result.Completed = true
					return result, nil
				},
			},
		}

		finalize := func(ctx context.Context, result models.InspectionResult) error {
			if result.SnapshotID != "" {
				log.Infow("removing VM snapshot", "vmId", vmID)
				req := vmware.RemoveSnapshotRequest{
					SnapshotId:  result.SnapshotID,
					Consolidate: true,
				}
				if err := operator.RemoveSnapshot(ctx, req); err != nil {
					log.Errorw("failed to remove VM snapshot", "vmId", vmID, "error", err)
				}
			}

			var status models.InspectionStatus
			switch {
			case result.Err != nil:
				status = models.InspectionStatus{State: models.InspectionStateError, Error: result.Err}
			case result.Completed:
				status = models.InspectionStatus{State: models.InspectionStateCompleted, Details: "completed"}
			default:
				status = models.InspectionStatus{State: models.InspectionStateCanceled, Details: "canceled"}
			}

			if err := store.Inspection().Update(ctx, vmID, status); err != nil {
				log.Errorw("failed to persist terminal inspection status", "vmId", vmID, "state", status.State, "error", err)
			}

			return nil
		}

		return work.NewSliceWorkBuilder2(units, finalize)
	}
}
