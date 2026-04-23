package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type (
	collectorPipeline        = WorkPipeline[models.CollectorStatus, models.CollectorResult]
	collectorWorkUnitBuilder func(creds models.Credentials) []models.WorkUnit[models.CollectorStatus, models.CollectorResult]
)

type CollectorService struct {
	scheduler    *scheduler.Scheduler[models.CollectorResult]
	pipeline     *collectorPipeline
	store        *store.Store
	eventSrv     *EventService
	inventorySrv *InventoryService

	dataDir        string
	opaPoliciesDir string
	buildFn        collectorWorkUnitBuilder
	mu             sync.Mutex
}

func NewCollectorService(st *store.Store, inventorySrv *InventoryService, eventSrv *EventService, dataDir, opaPoliciesDir string) *CollectorService {
	srv := &CollectorService{
		store:          st,
		inventorySrv:   inventorySrv,
		eventSrv:       eventSrv,
		dataDir:        dataDir,
		opaPoliciesDir: opaPoliciesDir,
	}

	return srv
}

func (c *CollectorService) GetStatus() models.CollectorStatus {
	inv, err := c.inventorySrv.GetInventory(context.Background())
	if err == nil && inv != nil {
		return models.CollectorStatus{State: models.CollectorStateCollected}
	}

	c.mu.Lock()
	pipeline := c.pipeline
	c.mu.Unlock()

	if pipeline != nil {
		state := pipeline.State()
		if state.Err == nil {
			return state.State
		}
		if !errors.Is(state.Err, errPipelineStopped) {
			return models.CollectorStatus{State: models.CollectorStateError, Error: state.Err}
		}
	}

	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (c *CollectorService) Start(ctx context.Context, creds models.Credentials) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pipeline != nil && c.pipeline.IsRunning() {
		return srvErrors.NewCollectionInProgressError()
	}

	inv, err := c.inventorySrv.GetInventory(ctx)
	if err == nil && inv != nil {
		return nil
	}

	if c.scheduler != nil {
		c.scheduler.Close()
	}

	sched, err := scheduler.NewScheduler[models.CollectorResult](1, 0)
	if err != nil {
		return err
	}
	c.scheduler = sched

	buildFn := c.buildWorkUnits
	if c.buildFn != nil {
		buildFn = c.buildFn
	}

	c.pipeline = NewWorkPipeline(models.CollectorStatus{State: models.CollectorStateConnecting}, sched, buildFn(creds))

	if err := c.pipeline.Start(); err != nil {
		c.pipeline = nil
		c.scheduler.Close()
		c.scheduler = nil
		return srvErrors.NewCollectionInProgressError()
	}
	return nil
}

// Stop detaches the current pipeline and scheduler under lock, then shuts them
// down outside the lock. This prevents Start from reusing instances that are in
// teardown while allowing GetStatus to fall back to the idle service state.
func (c *CollectorService) Stop() {
	c.mu.Lock()
	p := c.pipeline
	c.pipeline = nil
	s := c.scheduler
	c.scheduler = nil
	c.mu.Unlock()

	if p != nil {
		p.Stop()
	}
	if s != nil {
		s.Close()
	}
}

// WithWorkUnits overrides the default work unit builder. Intended for testing.
func (c *CollectorService) WithWorkUnits(fn collectorWorkUnitBuilder) *CollectorService {
	c.buildFn = fn
	return c
}

func (c *CollectorService) buildWorkUnits(creds models.Credentials) []models.WorkUnit[models.CollectorStatus, models.CollectorResult] {
	return []models.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, result models.CollectorResult) (models.CollectorResult, error) {
				err := c.verifyCredentials(ctx, creds)
				return result, err
			},
		},
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				sqlitePath, err := c.collect(ctx, creds)
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
				inv, err := c.process(ctx, r.SQLitePath)
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
				if err := c.eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
					return r, err
				}
				return r, nil
			},
		},
	}
}

func (c *CollectorService) verifyCredentials(ctx context.Context, cred models.Credentials) error {
	dbPath := path.Join(c.dataDir, fmt.Sprintf("%s.db", uuid.New()))
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

// collect verifies credentials, runs vSphere collection, and returns the sqlite DB path.
func (c *CollectorService) collect(ctx context.Context, creds models.Credentials) (string, error) {
	dbPath := path.Join(c.dataDir, fmt.Sprintf("%s.db", uuid.New()))
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

// process ingests collected sqlite data into DuckDB, builds and saves inventory,
// and creates folder groups.
func (c *CollectorService) process(ctx context.Context, sqlitePath string) ([]byte, error) {
	zap.S().Named("collector_service").Info("parsing collected data into duckdb")

	if _, err := os.Stat(sqlitePath); err != nil {
		zap.S().Named("collector_service").Errorw("sqlite file not accessible", "path", sqlitePath, "error", err)
		return nil, err
	}
	zap.S().Named("collector_service").Debugw("sqlite file ready", "path", sqlitePath)

	result, err := c.store.Parser().IngestSqlite(ctx, sqlitePath)
	if err != nil {
		zap.S().Named("collector_service").Errorw("failed to ingest sqlite data", "error", err)
		return nil, err
	}

	if err := c.store.Checkpoint(); err != nil {
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

	inv, err := c.store.Parser().BuildInventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("error building inventory: %w", err)
	}

	inventory, err := json.Marshal(converters.ToAPI(inv))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the inventory: %w", err)
	}

	if err := c.store.Inventory().Save(ctx, inventory); err != nil {
		return nil, err
	}

	zap.S().Named("inventory").Info("successfully created inventory with clusters")

	if err := c.createFolderGroups(ctx); err != nil {
		zap.S().Named("collector_service").Warnw("failed to create folder groups", "error", err)
	}

	return inventory, nil
}

func (c *CollectorService) createFolderGroups(ctx context.Context) error {
	folders, err := c.store.VM().GetFolders(ctx)
	if err != nil {
		return fmt.Errorf("getting folders: %w", err)
	}

	if err := c.store.WithTx(ctx, func(txCtx context.Context) error {
		for _, folder := range folders {
			group := models.Group{
				Name:        folder.Name,
				Description: fmt.Sprintf("VMs in folder: %s", folder.Name),
				Filter:      fmt.Sprintf("folder = '%s'", strings.ReplaceAll(folder.Name, `'`, `\'`)),
				Tags:        []string{sanitizeTag(folder.Name)},
			}
			if _, err := c.store.Group().Create(txCtx, group); err != nil {
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
		if _, err := c.store.Group().Create(txCtx, noFolderGroup); err != nil {
			zap.S().Named("collector_service").Warnw("failed to create no-folder group", "error", err)
		}

		return c.store.Group().RefreshMatches(txCtx)
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
