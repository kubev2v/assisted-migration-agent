package v1

import (
	"context"
	"fmt"
	"path"

	"go.uber.org/zap"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/collector"
)

// V1WorkBuilder builds a sequence of WorkUnits for the v1 collector workflow.
type V1WorkBuilder struct {
	collector      *collector.VSphereCollector
	store          *store.Store
	opaPoliciesDir string
	dataDir        string
	creds          *models.Credentials
}

// NewV1WorkBuilder creates a new v1 work builder.
func NewV1WorkBuilder(s *store.Store, dataDir, opaPoliciesDir string) *V1WorkBuilder {
	return &V1WorkBuilder{
		store:          s,
		opaPoliciesDir: opaPoliciesDir,
		dataDir:        dataDir,
	}
}

// WithCredentials sets the credentials for the workflow.
func (b *V1WorkBuilder) WithCredentials(creds *models.Credentials) models.WorkBuilder {
	b.creds = creds
	return b
}

// Build creates the sequence of WorkUnits for the collector workflow.
// The first unit is always the ready state.
func (b *V1WorkBuilder) Build() []models.WorkUnit {
	// create a new collector with a random sqlite db.
	// The db name needs to be unique per run because it cannot be reused.
	// It panics when the user stop and collect again but, because the collection step cannot be
	// stoped, it can happen that db can be full when the process stops.

	b.collector = collector.NewVSphereCollector(path.Join(b.dataDir, fmt.Sprintf("%s.db", uuid.New())))
	return []models.WorkUnit{
		b.connecting(),
		b.collecting(),
		b.collected(),
	}
}

func (b *V1WorkBuilder) connecting() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateConnecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("collector_service").Info("verifying vCenter credentials")
				if err := b.collector.VerifyCredentials(ctx, b.creds); err != nil {
					zap.S().Named("collector_service").Errorw("credential verification failed", "error", err)
					return nil, err
				}
				zap.S().Named("collector_service").Info("vCenter credentials verified")
				return nil, nil
			}
		},
	}
}

func (b *V1WorkBuilder) collecting() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("collector_service").Info("starting vSphere inventory collection")
				defer b.collector.Close()

				if err := b.collector.Collect(ctx, b.creds); err != nil {
					zap.S().Named("collector_service").Errorw("vSphere collection failed", "error", err)
					return nil, err
				}
				zap.S().Named("collector_service").Info("vSphere inventory collection completed")

				zap.S().Named("collector_service").Info("building inventory from collected data")
				processor := NewBuilder(b.store, b.opaPoliciesDir)
				if err := processor.Process(ctx, b.collector); err != nil {
					zap.S().Named("collector_service").Errorw("failed to build inventory", "error", err)
					return nil, err
				}
				zap.S().Named("collector_service").Info("inventory successfully processed")

				return nil, nil
			}
		},
	}
}

func (b *V1WorkBuilder) collected() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollected}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) { return nil, nil }
		},
	}
}
