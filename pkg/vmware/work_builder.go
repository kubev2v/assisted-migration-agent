package vmware

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// InsWorkBuilder builds a sequence of WorkUnits for the v1 Inspector workflow.
type InsWorkBuilder struct {
	operator VMOperator
}

// NewInspectorWorkBuilder creates a new v1 work builder.
func NewInspectorWorkBuilder(operator VMOperator) *InsWorkBuilder {
	return &InsWorkBuilder{
		operator: operator,
	}
}

// Build creates the sequence of WorkUnits for the Inspector workflow.
func (b *InsWorkBuilder) Build(id string) models.VMWorkflow {
	return b.vmWork(id)
}

func (b *InsWorkBuilder) vmWork(id string) models.VMWorkflow {
	return models.VMWorkflow{
		Validate:       b.validate(id),
		CreateSnapshot: b.createSnapshot(id),
		Inspect:        b.inspect(id),
		Save:           b.save(id),
		RemoveSnapshot: b.removeSnapshot(id),
	}
}

func (b *InsWorkBuilder) validate(id string) models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("inspector_service").Info("validate privileges on VM")

				if err := b.operator.ValidatePrivileges(ctx, id, models.RequiredPrivileges); err != nil {
					zap.S().Named("inspector_service").Errorw("validation failed", "error", err)
					return nil, err
				}

				return nil, nil
			}
		},
	}
}

func (b *InsWorkBuilder) createSnapshot(id string) models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("inspector_service").Infow("creating VM snapshot", "vmId", id)
				req := CreateSnapshotRequest{
					VmId:         id,
					SnapshotName: models.InspectionSnapshotName,
					Description:  "",
					Memory:       false,
					Quiesce:      false,
				}

				if err := b.operator.CreateSnapshot(ctx, req); err != nil {
					zap.S().Named("inspector_service").Errorw("failed to create VM snapshot", "vmId", id, "error", err)
					return nil, err
				}

				zap.S().Named("inspector_service").Infow("VM snapshot created", "vmId", id)

				return nil, nil
			}
		},
	}
}

func (b *InsWorkBuilder) inspect(id string) models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {

				return nil, nil
			}
		},
	}
}

func (b *InsWorkBuilder) save(id string) models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {

				return nil, nil
			}
		},
	}
}

func (b *InsWorkBuilder) removeSnapshot(id string) models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("inspector_service").Infow("removing VM snapshot", "vmId", id)

				removeSnapReq := RemoveSnapshotRequest{
					VmId:         id,
					SnapshotName: models.InspectionSnapshotName,
					Consolidate:  true,
				}

				if err := b.operator.RemoveSnapshot(ctx, removeSnapReq); err != nil {
					zap.S().Named("inspector_service").Errorw("failed to remove VM snapshot", "vmId", id, "error", err)
					return nil, err
				}

				zap.S().Named("inspector_service").Infow("VM snapshot removed", "vmId", id)

				return nil, nil
			}
		},
	}
}

type UnimplementedInspectionWorkBuilder struct{}

func (u UnimplementedInspectionWorkBuilder) Build(id string) models.VMWorkflow {
	return models.VMWorkflow{
		Validate: models.InspectorWorkUnit{
			Work: func() func(ctx context.Context) (any, error) {
				return UnimplementedVMWorkUnit(time.Second, "unimplemented Validate step finished for: %s", id)
			},
		},
		CreateSnapshot: models.InspectorWorkUnit{
			Work: func() func(ctx context.Context) (any, error) {
				return UnimplementedVMWorkUnit(time.Second, "unimplemented CreateSnapshot step finished for: %s", id)
			},
		},
		Inspect: models.InspectorWorkUnit{
			Work: func() func(ctx context.Context) (any, error) {
				return UnimplementedVMWorkUnit(time.Second, "unimplemented Inspect step finished for: %s", id)
			},
		},
		Save: models.InspectorWorkUnit{
			Work: func() func(ctx context.Context) (any, error) {
				return UnimplementedVMWorkUnit(time.Second, "unimplemented Save step finished for: %s", id)
			},
		},
		RemoveSnapshot: models.InspectorWorkUnit{
			Work: func() func(ctx context.Context) (any, error) {
				return UnimplementedVMWorkUnit(time.Second, "unimplemented RemoveSnapshot step finished for: %s", id)
			},
		},
	}
}

func UnimplementedVMWorkUnit(delay time.Duration, msg string, args ...string) func(ctx context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		time.Sleep(delay)
		if msg != "" {
			zap.S().Named("inspector_service").Infof(msg, args)
		}
		return nil, nil
	}
}
