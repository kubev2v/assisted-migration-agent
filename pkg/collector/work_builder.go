package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"

	"go.uber.org/zap"

	"github.com/google/uuid"

	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// WorkBuilder builds a sequence of WorkUnits for the v1 collector workflow.
type WorkBuilder struct {
	collector      *VSphereCollector
	store          *store.Store
	opaPoliciesDir string
	dataDir        string
	creds          *models.Credentials
}

// NewWorkBuilder creates a new v1 work builder.
func NewWorkBuilder(s *store.Store, dataDir, opaPoliciesDir string) *WorkBuilder {
	return &WorkBuilder{
		store:          s,
		opaPoliciesDir: opaPoliciesDir,
		dataDir:        dataDir,
	}
}

// WithCredentials sets the credentials for the workflow.
func (b *WorkBuilder) WithCredentials(creds *models.Credentials) models.WorkBuilder {
	b.creds = creds
	return b
}

// Build creates the sequence of WorkUnits for the collector workflow.
// The first unit is always the ready state.
func (b *WorkBuilder) Build() []models.WorkUnit {
	// create a new collector with a random sqlite db.
	// The db name needs to be unique per run because it cannot be reused.
	// It panics when the user stop and collect again but, because the collection step cannot be
	// stoped, it can happen that db can be full when the process stops.

	b.collector = NewVSphereCollector(path.Join(b.dataDir, fmt.Sprintf("%s.db", uuid.New())))
	return []models.WorkUnit{
		b.connecting(),
		b.collecting(),
		b.parsing(),
		b.collected(),
	}
}

func (b *WorkBuilder) connecting() models.WorkUnit {
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

func (b *WorkBuilder) collecting() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollecting}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				defer b.collector.Close()
				zap.S().Named("collector_service").Info("starting vSphere inventory collection")

				if err := b.collector.Collect(ctx, b.creds); err != nil {
					zap.S().Named("collector_service").Errorw("vSphere collection failed", "error", err)
					return nil, err
				}
				zap.S().Named("collector_service").Info("vSphere inventory collection completed")

				return nil, nil
			}
		},
	}
}

func (b *WorkBuilder) parsing() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateParsing}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) {
				zap.S().Named("collector_service").Info("parsing collected data into duckdb")

				sqlitePath := b.collector.DBPath()

				if _, err := os.Stat(sqlitePath); err != nil {
					zap.S().Named("collector_service").Errorw("sqlite file not accessible", "path", sqlitePath, "error", err)
					return nil, err
				}
				zap.S().Named("collector_service").Debugw("sqlite file ready", "path", sqlitePath)

				result, err := b.store.Parser().IngestSqlite(ctx, sqlitePath)
				if err != nil {
					zap.S().Named("collector_service").Errorw("failed to ingest sqlite data", "error", err)
					return nil, err
				}

				if err := b.store.Checkpoint(); err != nil {
					zap.S().Named("collector_service").Warnw("checkpoint after ingest failed", "error", err)
				}

				if result.HasErrors() {
					zap.S().Named("collector_service").Errorw("schema validation errors", "errors", result.Errors)
					return nil, fmt.Errorf("schema validation failed: %v", result.Errors)
				}

				if len(result.Warnings) > 0 {
					zap.S().Named("collector_service").Warnw("schema validation warnings", "warnings", result.Warnings)
				}

				zap.S().Named("collector_service").Info("data successfully parsed into duckdb")

				if err := os.Remove(sqlitePath); err != nil {
					zap.S().Named("collector_service").Warnw("failed to remove sqlite file", "path", sqlitePath, "error", err)
				}

				inv, err := b.store.Parser().BuildInventory(ctx)
				if err != nil {
					return nil, fmt.Errorf("error building inventory: %w", err)
				}

				// Store the inventory
				inventory, err := json.Marshal(converters.ToAPI(inv))
				if err != nil {
					return nil, fmt.Errorf("failed to marshal the inventory: %w", err)
				}

				if err := b.store.Inventory().Save(ctx, inventory); err != nil {
					return nil, err
				}

				zap.S().Named("inventory").Info("Successfully created inventory with clusters")

				// Create groups for each folder
				if err := b.createFolderGroups(ctx); err != nil {
					zap.S().Named("collector_service").Warnw("failed to create folder groups", "error", err)
				}

				return nil, nil
			}
		},
	}
}

// createFolderGroups creates a group for each folder and one for VMs without a folder.
func (b *WorkBuilder) createFolderGroups(ctx context.Context) error {
	folders, err := b.store.VM().GetFolders(ctx)
	if err != nil {
		return fmt.Errorf("getting folders: %w", err)
	}

	// Create group for each folder
	for _, folder := range folders {
		group := models.Group{
			Name:        folder.Name,
			Description: fmt.Sprintf("VMs in folder: %s", folder.Name),
			Filter:      fmt.Sprintf("folder = '%s'", folder.Name),
		}
		if _, err := b.store.Group().Create(ctx, group); err != nil {
			zap.S().Named("collector_service").Warnw("failed to create folder group",
				"folder", folder.Name, "error", err)
		}
	}

	// Create "No Folder" group for VMs without a folder
	noFolderGroup := models.Group{
		Name:        "No Folder",
		Description: "VMs not organized in any folder",
		Filter:      "folder = ''",
	}
	if _, err := b.store.Group().Create(ctx, noFolderGroup); err != nil {
		zap.S().Named("collector_service").Warnw("failed to create no-folder group", "error", err)
	}

	zap.S().Named("collector_service").Infow("folder groups created", "count", len(folders)+1)
	return nil
}

func (b *WorkBuilder) collected() models.WorkUnit {
	return models.WorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollected}
		},
		Work: func() func(ctx context.Context) (any, error) {
			return func(ctx context.Context) (any, error) { return nil, nil }
		},
	}
}
