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
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
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
// The pipeline executes 12 sequential work units against a dedicated collection
// DuckDB database (one per run). On completion, Finalize either promotes the
// collection DB into the pool (success), marks it failed (error), or cleans it
// up (cancelled).
//
// Pipeline stages:
//  1. Provision — record a collection marker, create and migrate the collection DB.
//  2. Verify — validate vCenter credentials and open a govmomi client for rightsizing.
//  3. Collect — run the vSphere collector, producing a SQLite database of raw inventory.
//  4. Ingest — import the SQLite output into the collection DuckDB, validate schema.
//  5. Applications — match guest processes against known application definitions.
//  6. Rightsizing:
//     6a. Rightsizing: create report — read VMs from inventory, create the report shell.
//     6b. Rightsizing: query + persist — query vCenter metrics, persist batches in a loop.
//     6c. Rightsizing: warnings — persist VMs that returned no metrics data.
//     6d. Rightsizing: utilization — compute per-VM utilization percentages.
//  7. Sync with the previous collection — copy groups, labels and exclusions from the previous collection.
//  8. Inventory — build the inventory JSON with embedded cluster utilization and persist.
//  9. Publish — write an inventory-update event to the outbox.
func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	log := zap.S().Named("collector_service")

	var collectionDb *store.Database
	var parser *duckdb_parser.Parser
	database := fmt.Sprintf("collection_%d", time.Now().Unix())

	var rsReportID string
	var rsVMs []VMInfo
	var rsVMResults map[string]VMReport
	var rsSvc *RightsizingService
	var rsWindowStart, rsWindowEnd time.Time

	units := []work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
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

				// since forklift collector does not expose the client
				// we need to create a separate client for rightsizing
				client, err := vmware.Connect(ctx, &creds)
				if err != nil {
					r.Err = err
					return r, err
				}

				r.Client = client
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
					return r, err
				}
				log.Info("vSphere inventory collection completed")

				r.SQLitePath = dbPath
				return r, nil
			},
		},
		// 4. Ingest: import the SQLite output into the collection DuckDB and validate schema.
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
		// 6a. Rightsizing — create report shell.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateMetricsCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					return r, fmt.Errorf("getting collection store: %w", err)
				}
				rsSvc = NewRightsizingService(st)

				id, vms, start, end, err := rsSvc.CreateReportFromInventory(ctx)
				if err != nil {
					return r, err
				}
				rsReportID = id
				rsVMs = vms
				rsWindowStart = start
				rsWindowEnd = end
				return r, nil
			},
		},
		// 6b. Rightsizing — query metrics, persist batches.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateMetricsCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				results, err := rsSvc.QueryMetrics(ctx, r.Client, rsVMs, rsWindowStart, rsWindowEnd)
				if err != nil {
					return r, err
				}
				rsVMResults = results

				if err := rsSvc.PersistMetrics(ctx, rsVMs, rsVMResults, rsReportID); err != nil {
					return r, err
				}
				return r, nil
			},
		},
		// 6c. Rightsizing — persist VM warnings.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateMetricsCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := rsSvc.PersistVMWarnings(ctx, rsVMs, rsVMResults, rsReportID); err != nil {
					return r, err
				}
				return r, nil
			},
		},
		// 6d. Rightsizing — compute per-VM utilization.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateMetricsCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := rsSvc.ComputeUtilization(ctx, rsReportID); err != nil {
					return r, err
				}
				return r, nil
			},
		},
		// 7. Sync user data from the previous collection into the new one.
		// Attach the (closed) new collection DB to the previous collection's connection,
		// run cross-DB SQL to copy groups, labels, and migration exclusion flags, then
		// detach and reopen the new DB to refresh group inventories.
		// If no previous collection exists, this stage is a no-op.
		// Any failure here fails the collection — a collection without user data (groups,
		// labels, exclusion flags) is considered invalid and should not be published.
		//
		// This runs before the Inventory stage so that the persisted/published inventory
		// (which embeds per-VM MigrationExcluded and Labels) reflects the synced data
		// instead of the new collection's pre-sync defaults.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, result models.CollectorResult) (models.CollectorResult, error) {
				prevDB, err := f.pool.Latest()
				if err != nil {
					if errors.IsResourceNotFoundError(err) {
						log.Info("no previous collection found, skipping sync")
						return result, nil
					}
					result.Err = err
					return result, err
				}

				log.Infow("syncing user data from previous collection", "previous_id", prevDB.ID)
				now := time.Now()

				prevSt, err := prevDB.Store()
				if err != nil {
					result.Err = fmt.Errorf("sync: failed to open previous collection store: %w", err)
					return result, result.Err
				}

				const attachedSchema = "new_col"

				// DuckDB requires the file to be closed before it can be attached to another connection.
				// collectionDb.Store() in subsequent steps transparently reopens it.
				if err := collectionDb.Close(); err != nil {
					result.Err = fmt.Errorf("sync: failed to close collection database before attach: %w", err)
					return result, result.Err
				}

				if err := prevSt.AttachDatabase(ctx, collectionDb, attachedSchema, store.ReadWriteDatabase); err != nil {
					result.Err = fmt.Errorf("sync: failed to attach collection database: %w", err)
					return result, result.Err
				}

				syncErr := SyncAttached(ctx, prevSt, attachedSchema, now)

				if detachErr := prevSt.DetachDatabase(ctx, attachedSchema); detachErr != nil {
					log.Errorw("failed to detach collection database", "error", detachErr)
					if syncErr == nil {
						syncErr = fmt.Errorf("sync: failed to detach collection database: %w", detachErr)
					}
				}

				if syncErr != nil {
					result.Err = syncErr
					return result, result.Err
				}

				// Reopen the collection DB (transparently reconnects after the Close() above)
				// and refresh group inventories against the new VM set.
				newSt, err := collectionDb.Store()
				if err != nil {
					result.Err = fmt.Errorf("sync: failed to reopen collection database for group refresh: %w", err)
					return result, result.Err
				}

				// The Close() above invalidated the connection parser was bound to at
				// provisioning time; rebind it to the reopened store so this and later
				// stages (e.g. Inventory) don't query a closed connection.
				parser = duckdb_parser.New(newSt.Querier(), f.validator)

				changedGroups, err := RefreshGroupInventories(ctx, newSt, NewGroupService(newSt, parser))
				if err != nil {
					result.Err = fmt.Errorf("sync: failed to refresh group inventories: %w", err)
					return result, result.Err
				}
				result.ChangedGroups = changedGroups

				log.Info("collection sync completed")
				return result, nil
			},
		},
		// 8. Inventory: build the inventory JSON with embedded cluster utilization and persist.
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
							MemAvg:     min(c.MemAvg, 100),
							MemP95:     min(c.MemP95, 100),
							MemMax:     min(c.MemMax, 100),
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
		// 9. Publish: write an inventory-update event to this collection's own outbox, plus an
		// upsert/delete event for every group whose inventory changed during stage 8's refresh.
		// The outbox lives per-collection (not in main) — mutations only ever happen against
		// the latest collection (see ServiceManager.Latest*Service()), and Console reads/clears
		// events via ServiceManager.LatestEventService(), which always resolves to this same DB.
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
				eventSrv := NewEventService(st)
				if err := eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
					r.Err = err
					return r, err
				}

				for i := range r.ChangedGroups {
					g := &r.ChangedGroups[i]
					if g.Inventory == nil {
						data, err := buildGroupInventoryDeleteEventData(g)
						if err != nil {
							r.Err = fmt.Errorf("building inventory delete event data for group %s: %w", g.ID, err)
							return r, r.Err
						}
						if err := eventSrv.AddGroupInventoryDeleteEvent(ctx, data); err != nil {
							r.Err = fmt.Errorf("adding group delete event for group %s: %w", g.ID, err)
							return r, r.Err
						}
						continue
					}

					data, err := buildGroupInventoryEventData(g)
					if err != nil {
						r.Err = fmt.Errorf("building inventory event data for group %s: %w", g.ID, err)
						return r, r.Err
					}
					if err := eventSrv.AddGroupInventoryEvent(ctx, data); err != nil {
						r.Err = fmt.Errorf("adding group inventory event for group %s: %w", g.ID, err)
						return r, r.Err
					}
				}

				r.Completed = true
				return r, nil
			},
		},
	}

	finalize := func(ctx context.Context, result models.CollectorResult) error {
		if result.Client != nil {
			_ = result.Client.Logout(context.Background())
		}

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
