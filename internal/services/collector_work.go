package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type collectorWorkFactory struct {
	store          *store.Store
	eventSrv       *EventService
	dataDir        string
	opaPoliciesDir string
}

func newCollectorWorkFactory(st *store.Store, eventSrv *EventService, dataDir, opaPoliciesDir string) *collectorWorkFactory {
	return &collectorWorkFactory{
		store:          st,
		eventSrv:       eventSrv,
		dataDir:        dataDir,
		opaPoliciesDir: opaPoliciesDir,
	}
}

func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	return work.NewSliceWorkBuilder([]collectorWorkUnit{
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, result models.CollectorResult) (models.CollectorResult, error) {
				err := f.verifyCredentials(ctx, creds)
				return result, err
			},
		},
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				sqlitePath, err := f.collect(ctx, creds)
				if err != nil {
					return r, err
				}
				r.SQLitePath = sqlitePath
				return r, nil
			},
		},
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				inv, err := f.process(ctx, r.SQLitePath)
				if err != nil {
					return r, err
				}
				r.Inventory = inv
				return r, nil
			},
		},
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollected}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := f.eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
					return r, err
				}
				return r, nil
			},
		},
	})
}

func (f *collectorWorkFactory) verifyCredentials(ctx context.Context, cred models.Credentials) error {
	dbPath := path.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
	vc := collector.NewVSphereCollector(dbPath)
	defer vc.Close()

	zap.S().Named("collector_service").Info("verifying vCenter credentials")
	if err := vc.VerifyCredentials(ctx, &cred); err != nil {
		zap.S().Named("collector_service").Errorw("credential verification failed", "error", err)
		return err
	}
	zap.S().Named("collector_service").Info("vCenter credentials verified")
	return nil
}

func (f *collectorWorkFactory) collect(ctx context.Context, creds models.Credentials) (string, error) {
	dbPath := path.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
	vc := collector.NewVSphereCollector(dbPath)
	defer vc.Close()

	zap.S().Named("collector_service").Info("starting vSphere inventory collection")
	if err := vc.Collect(ctx, &creds); err != nil {
		zap.S().Named("collector_service").Errorw("vSphere collection failed", "error", err)
		return "", err
	}
	zap.S().Named("collector_service").Info("vSphere inventory collection completed")

	return dbPath, nil
}

func (f *collectorWorkFactory) process(ctx context.Context, sqlitePath string) ([]byte, error) {
	zap.S().Named("collector_service").Info("parsing collected data into duckdb")

	if _, err := os.Stat(sqlitePath); err != nil {
		zap.S().Named("collector_service").Errorw("sqlite file not accessible", "path", sqlitePath, "error", err)
		return nil, err
	}
	zap.S().Named("collector_service").Debugw("sqlite file ready", "path", sqlitePath)

	result, err := f.store.Parser().IngestSqlite(ctx, sqlitePath)
	if err != nil {
		zap.S().Named("collector_service").Errorw("failed to ingest sqlite data", "error", err)
		return nil, err
	}

	if err := f.store.Checkpoint(); err != nil {
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

	inv, err := f.store.Parser().BuildInventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("error building inventory: %w", err)
	}

	inventory, err := json.Marshal(converters.ToAPI(inv))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the inventory: %w", err)
	}

	if err := f.store.Inventory().Save(ctx, inventory); err != nil {
		return nil, err
	}

	zap.S().Named("inventory").Info("successfully created inventory with clusters")

	if err := f.createFolderGroups(ctx); err != nil {
		zap.S().Named("collector_service").Warnw("failed to create folder groups", "error", err)
	}

	return inventory, nil
}

func (f *collectorWorkFactory) createFolderGroups(ctx context.Context) error {
	folders, err := f.store.VM().GetFolders(ctx)
	if err != nil {
		return fmt.Errorf("getting folders: %w", err)
	}

	if err := f.store.WithTx(ctx, func(txCtx context.Context) error {
		for _, folder := range folders {
			group := models.Group{
				Name:        folder.Name,
				Description: fmt.Sprintf("VMs in folder: %s", folder.Name),
				Filter:      fmt.Sprintf("folder = '%s'", strings.ReplaceAll(folder.Name, `'`, `\'`)),
				Tags:        []string{sanitizeTag(folder.Name)},
			}
			if _, err := f.store.Group().Create(txCtx, group); err != nil {
				zap.S().Named("collector_service").Warnw("failed to create folder group",
					"folder", folder.Name, "error", err)
			}
		}

		noFolderGroup := models.Group{
			Name:        "No Folder",
			Description: "VMs not organized in any folder",
			Filter:      "folder = ''",
			Tags:        []string{},
		}
		if _, err := f.store.Group().Create(txCtx, noFolderGroup); err != nil {
			zap.S().Named("collector_service").Warnw("failed to create no-folder group", "error", err)
		}

		return f.store.Group().RefreshMatches(txCtx)
	}); err != nil {
		return fmt.Errorf("creating folder groups: %w", err)
	}

	zap.S().Named("collector_service").Infow("folder groups created", "count", len(folders)+1)
	return nil
}

var tagSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.]`)

func sanitizeTag(name string) string {
	tag := strings.ReplaceAll(name, " ", "_")
	return tagSanitizer.ReplaceAllString(tag, "")
}
