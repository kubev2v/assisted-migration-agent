package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type collectorWorkFactory struct {
	store                 *store.Store
	eventSrv              *EventService
	dataDir               string
	opaPoliciesDir        string
	postCollectionBuilder postCollectionBuilderFn
}

func newCollectorWorkFactory(st *store.Store, eventSrv *EventService, dataDir, opaPoliciesDir string) *collectorWorkFactory {
	return &collectorWorkFactory{
		store:          st,
		eventSrv:       eventSrv,
		dataDir:        dataDir,
		opaPoliciesDir: opaPoliciesDir,
	}
}

// WithPostCollectionBuilder registers extra work units to be spliced into the
// pipeline immediately before the final "collected" event unit. Called after
// construction so that the rightsizing service can be wired in by the manager.
func (f *collectorWorkFactory) WithPostCollectionBuilder(fn postCollectionBuilderFn) *collectorWorkFactory {
	f.postCollectionBuilder = fn
	return f
}

func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	units := []collectorWorkUnit{
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
				zap.S().Named("collector_service").Info("ingesting sqlite data into duckdb")
				if err := f.ingestSqlite(ctx, r.SQLitePath); err != nil {
					zap.S().Named("collector_service").Errorw("ingest failed", "error", err)
					return r, err
				}
				zap.S().Named("collector_service").Info("sqlite data successfully ingested into duckdb")
				return r, nil
			},
		},
	}

	if f.postCollectionBuilder != nil {
		units = append(units, f.postCollectionBuilder(creds)...)
	}

	units = append(units, []collectorWorkUnit{
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				zap.S().Named("collector_service").Info("building inventory with utilization from duckdb")
				inv, err := f.buildAndMarshalInventory(ctx)
				if err != nil {
					zap.S().Named("collector_service").Errorw("failed to build inventory", "error", err)
					return r, err
				}

				if err := f.store.Inventory().Save(ctx, inv); err != nil {
					return r, err
				}

				zap.S().Named("inventory").Info("successfully created inventory with clusters")

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
	}...)

	return work.NewSliceWorkBuilder(units)
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

func (f *collectorWorkFactory) ingestSqlite(ctx context.Context, sqlitePath string) error {
	if _, err := os.Stat(sqlitePath); err != nil {
		zap.S().Named("collector_service").Errorw("sqlite file not accessible", "path", sqlitePath, "error", err)
		return err
	}
	zap.S().Named("collector_service").Debugw("sqlite file ready", "path", sqlitePath)

	result, err := f.store.Parser().IngestSqlite(ctx, sqlitePath)
	if err != nil {
		zap.S().Named("collector_service").Errorw("failed to ingest sqlite data", "error", err)
		return err
	}

	if err := f.store.Checkpoint(); err != nil {
		zap.S().Named("collector_service").Warnw("checkpoint after ingest failed", "error", err)
	}

	if result.HasErrors() {
		zap.S().Named("collector_service").Errorw("schema validation errors", "errors", result.Errors)
		return fmt.Errorf("schema validation failed: %v", result.Errors)
	}

	if len(result.Warnings) > 0 {
		zap.S().Named("collector_service").Warnw("schema validation warnings", "warnings", result.Warnings)
	}

	zap.S().Named("collector_service").Info("data successfully parsed into duckdb")

	if err := os.Remove(sqlitePath); err != nil {
		zap.S().Named("collector_service").Warnw("failed to remove sqlite file", "path", sqlitePath, "error", err)
	}

	return nil
}

func (f *collectorWorkFactory) buildAndMarshalInventory(ctx context.Context) ([]byte, error) {
	inv, err := f.store.Parser().BuildInventory(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error building inventory: %w", err)
	}

	zap.S().Named("collector_service").Info("attempting to embed cluster utilization into inventory")
	invCopy, err := f.embedClusterUtilization(ctx, *inv)
	if err != nil {
		zap.S().Named("collector_service").Warnw("failed to embed cluster utilization, continuing without it", "error", err)
	} else {
		inv = &invCopy
	}

	inventory, err := json.Marshal(converters.ToAPI(inv))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the inventory: %w", err)
	}

	return inventory, nil
}

// embedClusterUtilization fetches the latest cluster utilization data from rightsizing tables
// and populates each cluster's ClusterUtilization field.
// Only utilization percentages (CPU/memory avg/p95/max, confidence) are mapped for sizing calculations.
func (f *collectorWorkFactory) embedClusterUtilization(ctx context.Context, inv inventory.Inventory) (inventory.Inventory, error) {
	zap.S().Named("collector_service").Debug("fetching cluster utilization from rightsizing store")
	_, clusters, err := f.store.RightSizing().ListLatestClusterUtilization(ctx, "")
	if err != nil {
		zap.S().Named("collector_service").Errorw("error fetching cluster utilization from store", "error", err)
		return inventory.Inventory{}, fmt.Errorf("fetching cluster utilization: %w", err)
	}

	zap.S().Named("collector_service").Debugw("fetched cluster utilization from store", "cluster_count", len(clusters))

	if len(clusters) == 0 {
		zap.S().Named("collector_service").Debug("no rightsizing data available, inventory will not include cluster utilization")
		return inv, nil
	}

	// Create a new clusters map to avoid mutating the input
	newClusters := make(map[string]inventory.InventoryData, len(inv.Clusters))
	for clusterID, clusterData := range inv.Clusters {
		newClusters[clusterID] = clusterData
	}

	// Build map for O(1) lookup
	utilizationByClusterID := make(map[string]*inventory.ClusterUtilization, len(clusters))
	for _, c := range clusters {
		utilizationByClusterID[c.ClusterID] = &inventory.ClusterUtilization{
			CpuAvg:     sanitizePercentage("cpu_avg", c.CpuAvg),
			CpuP95:     sanitizePercentage("cpu_p95", c.CpuP95),
			CpuMax:     sanitizePercentage("cpu_max", c.CpuMax),
			MemAvg:     sanitizePercentage("mem_avg", c.MemAvg),
			MemP95:     sanitizePercentage("mem_p95", c.MemP95),
			MemMax:     sanitizePercentage("mem_max", c.MemMax),
			Confidence: sanitizePercentage("confidence", c.Confidence),
		}
	}

	// Embed utilization into each cluster's InventoryData
	embeddedCount := 0
	for clusterID := range newClusters {
		if util, exists := utilizationByClusterID[clusterID]; exists {
			clusterData := newClusters[clusterID]
			clusterData.ClusterUtilization = util
			newClusters[clusterID] = clusterData
			embeddedCount++
			zap.S().Named("collector_service").Debugw("embedded utilization for cluster",
				"cluster_id", clusterID,
				"cpu_p95", util.CpuP95,
				"mem_p95", util.MemP95,
				"confidence", util.Confidence)
		}
	}

	inv.Clusters = newClusters

	zap.S().Named("collector_service").Infow("embedded cluster utilization into inventory", "embedded_count", embeddedCount, "total_clusters", len(inv.Clusters))
	return inv, nil
}

func sanitizePercentage(name string, val float64) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		zap.S().Named("collector_service").Warnw("invalid utilization value, using 0",
			"field", name, "value", val)
		return 0
	}
	if val < 0 {
		zap.S().Named("collector_service").Warnw("utilization below 0%, correcting to 0",
			"field", name, "value", val)
		return 0
	}
	if val > 100 {
		zap.S().Named("collector_service").Warnw("utilization above 100%, correcting to 100",
			"field", name, "value", val)
		return 100
	}
	return val
}
