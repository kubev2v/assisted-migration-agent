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
	creds    *models.Credentials
	vmsMoid  []string
}

// NewInspectorWorkBuilder creates a new v1 work builder.
func NewInspectorWorkBuilder(cred *models.Credentials, vmsId []string) *InsWorkBuilder {
	return &InsWorkBuilder{
		creds:   cred,
		vmsMoid: vmsId,
	}
}

// Build creates the sequence of WorkUnits for the Inspector workflow.
func (b *InsWorkBuilder) Build() models.InspectorFlow {
	return models.InspectorFlow{
		Connect: b.connect(),
		Inspect: b.inspectVms(),
	}
}

func (b *InsWorkBuilder) connect() models.InspectorWorkUnit {
	return models.InspectorWorkUnit{
		Status: func() models.InspectorStatus {
			return models.InspectorStatus{State: models.InspectorStateConnecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("inspector_service").Info("connecting to vSphere")
				c, err := NewVsphereClient(ctx, b.creds.URL, b.creds.Username, b.creds.Password, true)
				if err != nil {
					zap.S().Named("inspector_service").Errorw("failed to connect to vSphere", "error", err)
					return nil, err
				}

				b.operator = NewVMManager(c)
				zap.S().Named("inspector_service").Info("vSphere connection established")

				return nil, nil
			}
		},
	}
}

func (b *InsWorkBuilder) inspectVms() []models.VmWorkUnit {
	var units []models.VmWorkUnit

	for _, vmMoid := range b.vmsMoid {
		moid := vmMoid // capture loop variable
		units = append(units, models.VmWorkUnit{
			VmMoid: moid,
			Work: models.InspectorWorkUnit{
				Status: func() models.InspectorStatus {
					return models.InspectorStatus{State: models.InspectorStateRunning}
				},
				Work: func() func(ctx context.Context) (any, error) {
					return func(ctx context.Context) (any, error) {
						zap.S().Named("inspector_service").Infow("creating VM snapshot", "vmMoid", moid)
						req := CreateSnapshotRequest{
							VmMoid:       moid,
							SnapshotName: models.InspectionSnapshotName,
							Description:  "",
							Memory:       false,
							Quiesce:      false,
						}

						if err := b.operator.CreateSnapshot(ctx, req); err != nil {
							zap.S().Named("inspector_service").Errorw("failed to create VM snapshot", "vmMoid", moid, "error", err)
							return nil, err
						}

						zap.S().Named("inspector_service").Infow("VM snapshot created", "vmMoid", moid)

						// Todo: add the inspection logic here
						time.Sleep(180 * time.Second)

						removeSnapReq := RemoveSnapshotRequest{
							VmMoid:       moid,
							SnapshotName: models.InspectionSnapshotName,
							Consolidate:  true,
						}

						if err := b.operator.RemoveSnapshot(ctx, removeSnapReq); err != nil {
							zap.S().Named("inspector_service").Errorw("failed to remove VM snapshot", "vmMoid", moid, "error", err)
							return nil, err
						}

						zap.S().Named("inspector_service").Infow("VM snapshot removed", "vmMoid", moid)

						return nil, nil
					}
				},
			},
		})

	}

	return units
}
