package v2

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type rvtoolsWorkFactory struct {
	pool         *store.Pool
	dataDir      string
	validator    *opa.Validator
	rvToolsFiles []string
}

func newRvtoolWorkFactory(pool *store.Pool, rvToolsFiles []string, dataDir string, validator *opa.Validator) (*rvtoolsWorkFactory, error) {
	return &rvtoolsWorkFactory{
		pool:         pool,
		dataDir:      dataDir,
		validator:    validator,
		rvToolsFiles: rvToolsFiles,
	}, nil
}

func (f *rvtoolsWorkFactory) Build() work.WorkBuilder2[models.CollectorStatus, models.CollectorResult] {
	log := zap.S().Named("collector_service")

	var collectionDb *store.Database
	var parser *duckdb_parser.Parser
	database := fmt.Sprintf("collection_%d", time.Now().Unix())

	units := []work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		// 1. Provision: record collection marker, create and migrate the collection DB.
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
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
				collectionDb, dbError = f.pool.NewDatabase(id, dbPath, time.Now(), store.EagerConnectionInitilization, 1024, store.ReadWriteDatabase)
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
	}

	// 2. Ingest: parse each RVTools Excel file into the collection DB.
	for _, file := range f.rvToolsFiles {
		units = append(units, work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				st, err := collectionDb.Store()
				if err != nil {
					r.Err = fmt.Errorf("getting collection store: %w", err)
					return r, r.Err
				}

				log.Infow("ingesting rvtools file", "file", file)
				result, err := parser.IngestRvTools(ctx, file)
				if err != nil {
					log.Errorw("failed to ingest rvtools file", "file", file, "error", err)
					r.Err = fmt.Errorf("ingesting rvtools file %s: %w", file, err)
					return r, r.Err
				}

				if result.HasErrors() {
					log.Errorw("rvtools validation failed", "file", file, "errors", result.Errors)
					r.Err = fmt.Errorf("validation failed for %s: %v", file, result.Errors)
					return r, r.Err
				}

				for _, w := range result.Warnings {
					log.Warnw("rvtools validation warning", "file", file, "code", w.Code, "message", w.Message)
				}

				if err := st.Checkpoint(ctx); err != nil {
					log.Errorw("checkpoint failed after ingesting", "file", file, "error", err)
					r.Err = fmt.Errorf("checkpoint after ingesting %s: %w", file, err)
					return r, r.Err
				}

				if err := os.Remove(file); err != nil {
					log.Warnw("failed to remove rvtools file", "file", file, "error", err)
				}

				log.Infow("rvtools file ingested", "file", file)
				return r, nil
			},
		})
	}

	// 3. Inventory: build the inventory JSON and persist.
	units = append(units, work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateParsing}
		},
		Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
			st, err := collectionDb.Store()
			if err != nil {
				r.Err = fmt.Errorf("getting collection store: %w", err)
				return r, r.Err
			}

			log.Info("building inventory from rvtools data")
			inv, err := parser.BuildInventory(ctx, nil)
			if err != nil {
				r.Err = fmt.Errorf("error building inventory: %w", err)
				return r, r.Err
			}

			invBytes, err := json.Marshal(converters.ToAPI(inv))
			if err != nil {
				r.Err = fmt.Errorf("failed to marshal inventory: %w", err)
				return r, r.Err
			}

			if err := st.Inventory().Save(ctx, invBytes); err != nil {
				r.Err = err
				return r, r.Err
			}

			log.Info("inventory created from rvtools data")
			r.Inventory = invBytes
			return r, nil
		},
	})

	// 4. Publish: write an inventory-update event to the outbox.
	units = append(units, work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateCollected}
		},
		Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
			r.Completed = true
			return r, nil
		},
	})

	finalize := func(ctx context.Context, result models.CollectorResult) error {
		for _, file := range f.rvToolsFiles {
			if err := os.Remove(file); err != nil {
				zap.S().Warnw("failed to remove rvtools file", "file", file, "error", err)
			}
		}

		if result.Err != nil {
			zap.S().Errorw("pipeline failed", "error", result.Err)
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
