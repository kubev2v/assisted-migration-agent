package v2

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type collectorWorkFactory struct {
	pool      *store.Pool
	dataDir   string
	validator *opa.Validator
}

func newCollectorWorkFactory(pool *store.Pool, dataDir string, validator *opa.Validator) (*collectorWorkFactory, error) {
	return &collectorWorkFactory{
		pool:      pool,
		dataDir:   dataDir,
		validator: validator,
	}, nil
}

// Build creates the collector work pipeline for a single collection run.
//
// The pipeline executes 9 sequential work units against a dedicated collection
// DuckDB database (one per run). On completion, Finalize either promotes the
// collection DB into the pool (success), marks it failed (error), or cleans it
// up (cancelled).
//
// Pipeline stages:
//  1. Provision — record a collection marker, create and migrate the collection DB.
//  2. Verify — validate vCenter credentials before committing to a full collection.
//  3. Collect — run the vSphere collector, producing a SQLite database of raw inventory.
//  4. Ingest — import the SQLite output into the collection DuckDB, validate schema.
//  5. Applications — match guest processes against known application definitions.
//  6. Rightsizing — query vCenter performance counters and persist utilization metrics.
//  7. Inventory — build the inventory JSON with embedded cluster utilization and persist.
//  8. Sync with the previous collection - If a previous collection exists, copy the groups,labels and exclude_migrations user data to the new collection.
//  9. Publish — write an inventory-update event to the outbox.
func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	log := zap.S().Named("collector_service")

	var collectionDb *store.Database
	var parser *duckdb_parser.Parser
	database := fmt.Sprintf("collection_%d", time.Now().Unix())

	units := []collectorWorkUnit{
		// 1. Provision: record collection marker, create and migrate the collection DB.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				mainDB, err := f.pool.Get(store.MainDatabaseID)
				if err != nil {
					r.Err = fmt.Errorf("getting main database: %w", err)
					return r, r.Err
				}
				mainStore, err := mainDB.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting main store: %w", err)
					return r, r.Err
				}

				if _, err := mainStore.Collection().Create(ctx, database); err != nil {
					r.Err = fmt.Errorf("creating collection marker for %s: %w", database, err)
					return r, r.Err
				}

				log.Infow("creating collection database", "name", database)
				dbPath := filepath.Join(f.dataDir, database+".duckdb")

				hash := sha256.Sum256([]byte(dbPath))
				id := hex.EncodeToString(hash[:])[:6]

				var dbError error
				collectionDb, dbError = f.pool.NewDatabase(id, dbPath, time.Now(), store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
				if dbError != nil {
					r.Err = fmt.Errorf("opening collection database %s: %w", database, dbError)
					return r, r.Err
				}

				if err := collectionDb.Migrate(ctx, func(ctx context.Context, sqlDb *sql.DB) error {
					st, err := collectionDb.Store()
					if err != nil {
						return err
					}

					parser = duckdb_parser.New(st.Querier(), f.validator)
					if err := parser.Init(); err != nil {
						return err
					}

					return migrations.RunCollection(ctx, sqlDb, database)
				}); err != nil {
					_ = collectionDb.Close()
					r.Err = fmt.Errorf("migrating collection database %s: %w", database, err)
					return r, r.Err
				}

				log.Infow("collection database ready", "name", database)
				return r, nil
			},
		},
		// 2. Verify: validate vCenter credentials before committing to a full collection.
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
		// 3. Collect: run the vSphere collector, producing a SQLite database of raw inventory.
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
		// 4. Ingest: import the SQLite output into the collection DuckDB and validate schema.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorLegacyStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}

				log.Info("ingesting sqlite data into duckdb")

				if _, err := os.Stat(r.SQLitePath); err != nil {
					log.Errorw("sqlite file not accessible", "path", r.SQLitePath, "error", err)
					r.Err = err
					return r, err
				}

				result, err := parser.IngestSqlite(ctx, r.SQLitePath)
				if err != nil {
					log.Errorw("failed to ingest sqlite data", "error", err)
					r.Err = err
					return r, err
				}

				if err := st.Checkpoint(ctx); err != nil {
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
		// 5. Applications: match guest processes against known application definitions.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}
				appSrv, err := NewApplicationService(st)
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
		// 6. Rightsizing: query vCenter performance counters and persist utilization metrics.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}
				rsSrv := NewRightsizingService(st)
				var workErr error
				for _, u := range rsSrv.BuildCollectorWorkUnits(
					rightsizingDefaultLookbackHours,
					rightsizingDefaultIntervalSeconds,
					rightsizingDefaultBatchSize,
				)(creds) {
					r, workErr = u.Work(ctx, r)
					if workErr != nil {
						r.Err = workErr
						return r, workErr
					}
				}
				return r, nil
			},
		},
		// 7. Inventory: build the inventory JSON with embedded cluster utilization and persist.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}

				log.Info("building inventory with utilization from duckdb")

				inv, err := parser.BuildInventory(ctx, nil)
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
							// Values are already safe: the SQL query uses NULLIF to avoid division-by-zero,
							// and sql.NullFloat64 maps NULL to 0.
							CpuAvg:     c.CpuAvg,
							CpuP95:     c.CpuP95,
							CpuMax:     c.CpuMax,
							MemAvg:     c.MemAvg,
							MemP95:     c.MemP95,
							MemMax:     c.MemMax,
							Confidence: c.Confidence,
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
		// 8. Sync data between the previous database and the current one.
		// - Copy groups and recompute the inventory for each group.
		// - Copy labels and exclude_migrations from the previous database to the new one.
		// - Add "first seen/new" label for virtual machines found on current database but not found on the previous one.
		// If previous is not found than is no-op.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, result models.CollectorResult) (models.CollectorResult, error) {
				var previousDatabase *store.Database
				for db := range f.pool.All() {
					previousDatabase = db
					break
				}

				if previousDatabase == nil {
					return result, nil
				}

				zap.S().Infow("found previous database", "database", previousDatabase.Path, "id", previousDatabase.ID)

				// close the current one, otherwise attaching this db to the previousDatabase will fail.
				// The connection will be reopen at next call of Store().
				if err := collectionDb.Close(); err != nil {
					result.Err = err
					return result, err
				}

				// TODO: Close the current one, attaching to the previous and copy data from previous to current.
				// Question: Should we have a dedicated method in the store for that?
				return result, nil
			},
		},
		// 9. Publish: write an inventory-update event to the outbox.
		// TODO: Question for Ami:
		// Where the Outbox lives?
		// 1. main
		// It could be main but it means we need to be careful with creating of groups and vm exclusion from an previous collection.
		// Scenario: the user creates a group on a old collection, which imo it does make sense from buisness pov but let's accept the argument.
		// the current code, updates the inventory and write it into the outbox to be sent to saas. Therefore, if we move the outbox in main the user could sent an obsolete inventory to saas.
		// 2. It leaves in its own collection:
		// In this case it's safe to update groups but the event service must be smart enough to read the outbox only from the latest collection.
		// I prefer 2nd because it is easier to implement. Console service gets the Event Service instnace which gets the pool instead of a main store.
		// So every time, console asks event service for the events, event service checks the pool for the latest collection.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollected}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}
				if err := st.Outbox().Insert(ctx, models.Event{
					Kind: models.InventoryUpdateEvent,
					Data: r.Inventory,
				}); err != nil {
					r.Err = err
					return r, err
				}
				r.Completed = true
				return r, nil
			},
		},
	}

	finalize := func(ctx context.Context, result models.CollectorResult) error {
		if database == "" {
			return nil
		}

		if collectionDb != nil {
			st, err := collectionDb.Store()
			if err == nil {
				if err := st.Checkpoint(ctx); err != nil {
					zap.S().Warnw("failed to checkpoint", "error", err)
				}
			}
		}

		mainDB, err := f.pool.Get(store.MainDatabaseID)
		if err != nil {
			return fmt.Errorf("failed to get main database: %w", err)
		}
		mainSt, err := mainDB.Store()
		if err != nil {
			return fmt.Errorf("failed to get main store: %w", err)
		}

		switch {
		case result.Completed:
			f.pool.Add(collectionDb)
			if err := mainSt.Collection().Delete(ctx, database); err != nil {
				zap.S().Warnw("failed to delete collection marker", "error", err)
			}
			zap.S().Infow("collection database added to pool", "id", collectionDb.ID, "path", collectionDb.Path)
		case result.Err != nil:
			if err := mainSt.Collection().MarkFailed(ctx, database, result.Err.Error()); err != nil {
				zap.S().Warnw("failed to mark collection as failed", "error", err)
			}
			if collectionDb != nil {
				_ = collectionDb.Close()
				if err := os.Remove(collectionDb.Path); err != nil {
					zap.S().Warnw("failed to remove collection database file", "error", err)
				}
			}
		default:
			if collectionDb != nil {
				_ = collectionDb.Close()
				if err := os.Remove(collectionDb.Path); err != nil {
					zap.S().Warnw("failed to remove collection database file", "error", err)
				}
			}
			if err := mainSt.Collection().Delete(ctx, database); err != nil {
				zap.S().Warnw("failed to delete collection marker", "error", err)
			}
		}
		return nil
	}

	return work.NewSliceWorkBuilder2(units, finalize)
}
