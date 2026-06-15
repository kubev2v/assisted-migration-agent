package services

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

type inspectionBuilder struct {
	vmID     string
	store    *store.Store
	operator vmware.VMOperator
	detector *vmdetect.Detector
	units    []work.WorkUnit[models.InspectionStatus, models.InspectionResult]
	idx      int
}

func (b *inspectionBuilder) Next() (work.WorkUnit[models.InspectionStatus, models.InspectionResult], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[models.InspectionStatus, models.InspectionResult]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *inspectionBuilder) Finalize(ctx context.Context, result models.InspectionResult) error {
	log := zap.S().Named("inspection_builder")

	if result.SnapshotID != "" {
		log.Infow("removing VM snapshot", "vmId", b.vmID)
		req := vmware.RemoveSnapshotRequest{
			SnapshotId:  result.SnapshotID,
			Consolidate: true,
		}
		if err := b.operator.RemoveSnapshot(ctx, req); err != nil {
			log.Errorw("failed to remove VM snapshot", "vmId", b.vmID, "error", err)
		}
	}

	var status models.InspectionStatus
	switch {
	case result.Err != nil:
		status = models.InspectionStatus{State: models.InspectionStateError, Error: result.Err, Details: ""}
	case result.Completed:
		status = models.InspectionStatus{State: models.InspectionStateCompleted, Details: "completed"}
	default:
		status = models.InspectionStatus{State: models.InspectionStateCanceled, Details: "canceled"}
	}

	if err := b.store.Inspection().Update(ctx, b.vmID, status); err != nil {
		log.Errorw("failed to persist terminal inspection status", "vmId", b.vmID, "state", status.State, "error", err)
	}

	return nil
}

func defaultInspectionBuilderFactory(s *store.Store, operator vmware.VMOperator, detector *vmdetect.Detector) inspectionBuilderFactory {
	return func(id string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
		log := zap.S().Named("inspection_builder")
		inspection := s.Inspection()

		return &inspectionBuilder{
			vmID:     id,
			store:    s,
			operator: operator,
			detector: detector,
			units: []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
				{
					Status: func() models.InspectionStatus {
						status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating credentials"}
						if err := inspection.Update(context.Background(), id, status); err != nil {
							log.Errorw("failed to persist status", "vmId", id, "error", err)
						}
						return status
					},
					Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
						if result.Err != nil {
							return result, nil
						}

						log.Infow("validating VM privileges", "vmId", id)
						if err := operator.ValidatePrivileges(ctx, id, models.RequiredPrivileges); err != nil {
							log.Errorw("privilege validation failed", "vmId", id, "error", err)
							result.Err = err
							return result, nil
						}
						log.Infow("privilege validation passed", "vmId", id)
						return result, nil
					},
				},
				{
					Status: func() models.InspectionStatus {
						status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating snapshot"}
						if err := inspection.Update(context.Background(), id, status); err != nil {
							log.Errorw("failed to persist status", "vmId", id, "error", err)
						}
						return status
					},
					Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
						log.Infow("creating VM snapshot", "vmId", id)
						snapID, err := operator.CreateSnapshot(ctx, vmware.CreateSnapshotRequest{
							VmId:         id,
							SnapshotName: models.InspectionSnapshotName,
						})
						if err != nil {
							log.Errorw("failed to create VM snapshot", "vmId", id, "error", err)
							result.Err = err
							return result, nil
						}
						result.SnapshotID = snapID
						log.Infow("VM snapshot created", "vmId", id)

						return result, nil
					},
				},
				{
					Status: func() models.InspectionStatus {
						status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "running deep inspection"}
						if err := inspection.Update(context.Background(), id, status); err != nil {
							log.Errorw("failed to persist status", "vmId", id, "error", err)
						}
						return status
					},
					Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
						log.Infow("running deep inspection", "vmId", id, "snapshotId", result.SnapshotID)
						detectResult, err := detector.Detect(vmdetect.DetectParams{
							Ctx:           ctx,
							VMMoref:       id,
							SnapshotMoref: result.SnapshotID,
						})
						if err != nil {
							log.Errorw("deep inspection failed", "vmId", id, "snapshotId", result.SnapshotID, "error", err)
							result.Err = err
							return result, nil
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

						log.Infow("deep inspection completed", "vmId", id, "concernCount", len(result.Concerns))
						return result, nil
					},
				},
				{
					Status: func() models.InspectionStatus {
						status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting results"}
						if err := inspection.Update(context.Background(), id, status); err != nil {
							log.Errorw("failed to persist status", "vmId", id, "error", err)
						}
						return status
					},
					Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
						if result.Err != nil {
							return result, nil
						}
						log.Infow("persisting inspection results", "vmId", id, "concernCount", len(result.Concerns))
						err := s.WithTx(ctx, func(txCtx context.Context) error {
							return s.Inspection().InsertResult(txCtx, id, result.Concerns)
						})
						if err != nil {
							log.Errorw("failed to persist inspection results", "vmId", id, "error", err)
							result.Err = err
							return result, nil
						}
						log.Infow("inspection results persisted", "vmId", id)
						result.Completed = true
						return result, nil
					},
				},
			},
		}
	}
}
