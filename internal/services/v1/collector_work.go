package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type collectorWorkBuilder struct {
	store           *store.Store
	collectionStore *store.Store
	dataDir         string
	database        string
	units           []collectorWorkUnit
	idx             int
}

func (b *collectorWorkBuilder) Next() (collectorWorkUnit, bool) {
	if b.idx >= len(b.units) {
		return collectorWorkUnit{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *collectorWorkBuilder) Finalize(ctx context.Context, result models.CollectorResult) error {
	if b.database == "" {
		return nil
	}

	if b.collectionStore != nil {
		if err := b.collectionStore.Checkpoint(); err != nil {
			zap.S().Warnw("failed to checkpoint", "error", err)
		}
		if err := b.collectionStore.Close(); err != nil {
			zap.S().Warnw("failed to close connection to the collection database", "error", err)
		}
		b.collectionStore = nil
	}

	dbFile := filepath.Join(b.dataDir, b.database+".duckdb")
	switch {
	case result.Completed:
		if err := b.store.AttachDatabase(ctx, b.dataDir, b.database); err != nil {
			return fmt.Errorf("attaching collection %s to default store: %w", b.database, err)
		}
		if err := b.store.Collection().Delete(ctx, b.database); err != nil {
			zap.S().Warnw("failed to delete collection marker", "error", err)
		}
		zap.S().Infow("database attached", "database", b.database)
	case result.Err != nil:
		if err := b.store.Collection().MarkFailed(ctx, b.database, result.Err.Error()); err != nil {
			zap.S().Warnw("failed to mark the collection as failed", "error", err)
		}
		if err := os.Remove(dbFile); err != nil {
			zap.S().Warnw("failed to remove database file", "error", err)
		}
	default:
		if err := os.Remove(dbFile); err != nil {
			zap.S().Warnw("failed to remove database file", "error", err)
		}
		if err := b.store.Collection().Delete(ctx, b.database); err != nil {
			zap.S().Warnw("failed to delete collection marker", "error", err)
		}
	}
	return nil
}

type collectorWorkFactory struct {
	store          *store.Store
	dataDir        string
	opaPoliciesDir string
}

func newCollectorWorkFactory(st *store.Store, dataDir, opaPoliciesDir string) *collectorWorkFactory {
	return &collectorWorkFactory{
		store:          st,
		dataDir:        dataDir,
		opaPoliciesDir: opaPoliciesDir,
	}
}

func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	log := zap.S().Named("collector_service")

	wb := &collectorWorkBuilder{
		store:    f.store,
		dataDir:  f.dataDir,
		database: fmt.Sprintf("collection_%d", time.Now().Unix()),
	}

	wb.units = []collectorWorkUnit{
		// 1. Create a dedicated collection database and run collection migrations.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if _, err := f.store.Collection().Create(ctx, wb.database); err != nil {
					r.Err = fmt.Errorf("creating collection marker for %s: %w", wb.database, err)
					return r, err
				}

				log.Infow("creating collection store", "name", wb.database)

				dbPath := filepath.Join(f.dataDir, wb.database+".duckdb")
				db, err := store.NewConnection(store.NewDefaultExtentionLoader(), dbPath)
				if err != nil {
					r.Err = fmt.Errorf("opening collection database %s: %w", wb.database, err)
					return r, err
				}

				validator, err := opa.NewValidatorFromDir(f.opaPoliciesDir)
				if err != nil {
					_ = db.Close()
					r.Err = fmt.Errorf("initializing OPA validator for %s: %w", wb.database, err)
					return r, err
				}

				collStore := store.NewStore(db, validator)
				if err := collStore.InitCollection(ctx); err != nil {
					_ = collStore.Close()
					r.Err = fmt.Errorf("initializing collection store %s: %w", wb.database, err)
					return r, err
				}

				wb.collectionStore = collStore
				log.Infow("collection store ready", "name", wb.database)
				return r, nil
			},
		},
		// 2. Verify vCenter credentials before starting the full collection.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				dbPath := filepath.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
				vc := collector.NewVSphereCollector(dbPath)
				defer vc.Close()

				log.Info("verifying vCenter credentials")
				if err := vc.VerifyCredentials(ctx, &creds); err != nil {
					log.Errorw("credential verification failed", "error", err)
					r.Err = err
					return r, err
				}
				log.Info("vCenter credentials verified")
				return r, nil
			},
		},
		// 3. Run the vSphere collector and produce a SQLite database.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if r.Err != nil {
					return r, nil
				}

				dbPath := path.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
				vc := collector.NewVSphereCollector(dbPath)
				defer vc.Close()

				log.Info("starting vSphere inventory collection")
				if err := vc.Collect(ctx, &creds); err != nil {
					log.Errorw("vSphere collection failed", "error", err)
					r.Err = err
					return r, nil
				}
				log.Info("vSphere inventory collection completed")

				r.SQLitePath = dbPath
				return r, nil
			},
		},
		// 4. Ingest the SQLite output into the collection DuckDB store.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st := wb.collectionStore
				log.Info("ingesting sqlite data into duckdb")

				if _, err := os.Stat(r.SQLitePath); err != nil {
					log.Errorw("sqlite file not accessible", "path", r.SQLitePath, "error", err)
					r.Err = err
					return r, err
				}

				result, err := st.Parser().IngestSqlite(ctx, r.SQLitePath)
				if err != nil {
					log.Errorw("failed to ingest sqlite data", "error", err)
					r.Err = err
					return r, err
				}

				if err := st.Checkpoint(); err != nil {
					log.Warnw("checkpoint after ingest failed", "error", err)
					r.Err = fmt.Errorf("checkpoint failed: %w", err)
					return r, err
				}

				if result.HasErrors() {
					log.Errorw("schema validation errors", "errors", result.Errors)
					r.Err = fmt.Errorf("schema validation failed: %v", result.Errors)
					return r, r.Err
				}

				if len(result.Warnings) > 0 {
					log.Warnw("schema validation warnings", "warnings", result.Warnings)
				}

				log.Info("sqlite data successfully ingested into duckdb")

				if err := os.Remove(r.SQLitePath); err != nil {
					log.Warnw("failed to remove sqlite file", "path", r.SQLitePath, "error", err)
				}

				return r, nil
			},
		},
		// 5. Detect applications by matching guest app names against known definitions.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				appSrv, err := NewApplicationService(wb.collectionStore)
				if err != nil {
					log.Warnw("skipping application detection", "error", err)
					r.Err = fmt.Errorf("failed to initiate application service: %w", err)
					return r, err
				}
				if err := appSrv.MatchApplications(ctx); err != nil {
					r.Err = err
					return r, err
				}
				return r, nil
			},
		},
		// 6. Collect rightsizing metrics from vCenter for the inventoried VMs.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateRightsizingConnecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				rsSrv := NewRightsizingService(wb.collectionStore)
				var err error
				for _, u := range rsSrv.BuildCollectorWorkUnits(
					rightsizingDefaultLookbackHours,
					rightsizingDefaultIntervalSeconds,
					rightsizingDefaultBatchSize,
				)(creds) {
					r, err = u.Work(ctx, r)
					if err != nil {
						r.Err = err
						return r, err
					}
				}
				return r, nil
			},
		},
		// 7. Build the inventory JSON, embed cluster utilization, and persist.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st := wb.collectionStore
				log.Info("building inventory with utilization from duckdb")

				inv, err := st.Parser().BuildInventory(ctx, nil)
				if err != nil {
					log.Errorw("failed to build inventory", "error", err)
					r.Err = fmt.Errorf("error building inventory: %w", err)
					return r, err
				}

				_, clusters, clErr := st.RightSizing().ListLatestClusterUtilization(ctx, "")
				if clErr != nil {
					log.Warnw("failed to fetch cluster utilization, continuing without it", "error", clErr)
				} else if len(clusters) > 0 {
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
					embeddedCount := 0
					for clusterID := range inv.Clusters {
						if util, exists := utilizationByClusterID[clusterID]; exists {
							clusterData := inv.Clusters[clusterID]
							clusterData.ClusterUtilization = util
							inv.Clusters[clusterID] = clusterData
							embeddedCount++
						}
					}
					log.Infow("embedded cluster utilization into inventory", "embedded_count", embeddedCount, "total_clusters", len(inv.Clusters))
				}

				invBytes, err := json.Marshal(converters.ToAPI(inv))
				if err != nil {
					r.Err = fmt.Errorf("failed to marshal the inventory: %w", err)
					return r, err
				}

				if err := st.Inventory().Save(ctx, invBytes); err != nil {
					r.Err = err
					return r, err
				}

				log.Info("successfully created inventory with clusters")
				r.Inventory = invBytes
				return r, nil
			},
		},
		// 8. Publish the inventory update event to the outbox.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollected}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				eventSrv := NewEventService(wb.collectionStore)
				if err := eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
					r.Err = err
					return r, err
				}
				r.Completed = true
				return r, nil
			},
		},
	}

	return wb
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
