package v2

import (
	"context"
	"fmt"
	"time"

	"github.com/vmware/govmomi/object"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type forecastBuilderFactory = func(pair models.DatastorePair) work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult]

func defaultForecastBuilderFactory(dm *vmware.DiskManager, st *store.Store2, strategyFn func() BenchmarkStrategy, diskSizeGB, iterations int, sessionID int64) forecastBuilderFactory {
	return func(pair models.DatastorePair) work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult] {
		b := &forecastBuilder{
			diskManager: dm,
			store:       st,
			strategy:    strategyFn(),
			pair:        pair,
			diskSizeGB:  diskSizeGB,
			iterations:  iterations,
			sessionID:   sessionID,
		}
		return b.build()
	}
}

type forecastBuilder struct {
	diskManager *vmware.DiskManager
	store       *store.Store2
	strategy    BenchmarkStrategy
	pair        models.DatastorePair
	diskSizeGB  int
	iterations  int
	sessionID   int64

	dc           *object.Datacenter
	selectedHost string
	tempDir      string
	srcPath      string
	dstPath      string
	prepDuration time.Duration
}

func (b *forecastBuilder) status(state models.ForecastPairState) models.ForecastPairStatus {
	return models.ForecastPairStatus{
		State:           state,
		PairName:        b.pair.Name,
		SourceDatastore: b.pair.SourceDatastore,
		TargetDatastore: b.pair.TargetDatastore,
		Host:            b.selectedHost,
		TotalRuns:       b.iterations,
	}
}

func (b *forecastBuilder) build() *work.SliceWorkBuilder2[models.ForecastPairStatus, models.ForecastResult] {
	units := []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
		{
			Status: func() models.ForecastPairStatus { return b.status(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")
				log.Infow("validating datastores", "pair", b.pair.Name,
					"source", b.pair.SourceDatastore, "target", b.pair.TargetDatastore)

				dc, err := b.diskManager.FindDatacenterForDatastore(ctx, b.pair.SourceDatastore)
				if err != nil {
					return result, fmt.Errorf("failed to find datacenter: %w", err)
				}
				b.dc = dc

				if err := b.diskManager.DatastoreExists(ctx, dc, b.pair.SourceDatastore); err != nil {
					return result, fmt.Errorf("source datastore validation failed: %w", err)
				}
				if err := b.diskManager.DatastoreExists(ctx, dc, b.pair.TargetDatastore); err != nil {
					return result, fmt.Errorf("target datastore validation failed: %w", err)
				}

				log.Infow("datastores validated", "pair", b.pair.Name)
				return result, nil
			},
		},
		{
			Status: func() models.ForecastPairStatus { return b.status(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				if err := b.strategy.Setup(ctx, b.dc, b.pair); err != nil {
					return result, fmt.Errorf("strategy setup failed: %w", err)
				}
				b.selectedHost = b.strategy.SelectedHost()
				return result, nil
			},
		},
		{
			Status: func() models.ForecastPairStatus { return b.status(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")
				b.tempDir = fmt.Sprintf("forecaster-%s-%d", b.pair.Name, time.Now().UnixNano())
				b.srcPath = fmt.Sprintf("%s/%s", b.tempDir, "benchmark-disk.vmdk")
				b.dstPath = fmt.Sprintf("%s/%s", b.tempDir, "benchmark-disk-clone.vmdk")

				log.Infow("creating benchmark directories", "pair", b.pair.Name, "dir", b.tempDir)
				if err := b.diskManager.CreateDirectory(ctx, b.dc, b.pair.SourceDatastore, b.tempDir); err != nil {
					return result, fmt.Errorf("failed to create source directory: %w", err)
				}
				if b.pair.SourceDatastore != b.pair.TargetDatastore {
					if err := b.diskManager.CreateDirectory(ctx, b.dc, b.pair.TargetDatastore, b.tempDir); err != nil {
						return result, fmt.Errorf("failed to create target directory: %w", err)
					}
				}
				return result, nil
			},
		},
		{
			Status: func() models.ForecastPairStatus {
				s := b.status(models.ForecastPairStatePreparing)
				s.PrepBytesTotal = int64(b.diskSizeGB) * 1024 * 1024 * 1024
				return s
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")
				prepStart := time.Now()

				log.Infow("creating benchmark disk", "pair", b.pair.Name, "sizeGB", b.diskSizeGB)
				if err := b.diskManager.CreateDisk(ctx, b.dc, b.pair.SourceDatastore, b.tempDir, "benchmark-disk.vmdk", b.diskSizeGB); err != nil {
					return result, fmt.Errorf("failed to create disk: %w", err)
				}

				log.Infow("filling disk with random data", "pair", b.pair.Name, "sizeGB", b.diskSizeGB)
				if err := b.strategy.FillDisk(ctx, b.dc, b.pair, b.srcPath, b.diskSizeGB, nil); err != nil {
					return result, fmt.Errorf("failed to fill disk: %w", err)
				}

				b.prepDuration = time.Since(prepStart)
				log.Infow("prep phase complete", "pair", b.pair.Name,
					"prepDuration", b.prepDuration.Round(time.Second))

				return result, nil
			},
		},
	}

	for iter := range b.iterations {
		i := iter + 1
		units = append(units, work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
			Status: func() models.ForecastPairStatus {
				s := b.status(models.ForecastPairStateRunning)
				s.CompletedRuns = iter
				return s
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")
				log.Infow("benchmark iteration", "pair", b.pair.Name, "iteration", i, "of", b.iterations)

				run := models.BenchmarkRun{
					SessionID:  b.sessionID,
					PairName:   b.pair.Name,
					SourceDS:   b.pair.SourceDatastore,
					TargetDS:   b.pair.TargetDatastore,
					Iteration:  i,
					DiskSizeGB: b.diskSizeGB,
					Method:     b.strategy.Name(),
				}
				if i == 1 {
					run.PrepDurationSec = b.prepDuration.Seconds()
				}

				benchResult, err := b.strategy.RunBenchmark(ctx, b.dc, b.pair, b.srcPath, b.dstPath, b.diskSizeGB)
				if err != nil {
					run.Error = fmt.Sprintf("benchmark failed: %v", err)
					run.DurationSec = benchResult.Duration.Seconds()
				} else {
					run.DurationSec = benchResult.Duration.Seconds()
					if run.DurationSec > 0 {
						run.ThroughputMBps = float64(b.diskSizeGB*1024) / run.DurationSec
					}
				}

				cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if delErr := b.diskManager.DeleteDisk(cleanCtx, b.dc, b.pair.TargetDatastore, b.dstPath); delErr != nil {
					log.Debugw("cleanup: failed to delete clone disk", "path", b.dstPath, "error", delErr)
				}

				result.Runs = append(result.Runs, run)

				if err := b.store.WithTx(ctx, func(txCtx context.Context) error {
					return b.store.Forecast().InsertRun(txCtx, run)
				}); err != nil {
					log.Errorw("failed to persist benchmark run", "pair", b.pair.Name, "iteration", i, "error", err)
				}

				if run.Error != "" {
					log.Infow("benchmark iteration failed", "pair", b.pair.Name, "iteration", i, "error", run.Error)
				} else {
					log.Infow("benchmark iteration complete", "pair", b.pair.Name,
						"iteration", i,
						"duration_sec", fmt.Sprintf("%.1f", run.DurationSec),
						"throughput_mbps", fmt.Sprintf("%.1f", run.ThroughputMBps))
				}

				return result, nil
			},
		})
	}

	units = append(units, work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
		Status: func() models.ForecastPairStatus {
			s := b.status(models.ForecastPairStateCompleted)
			s.CompletedRuns = b.iterations
			return s
		},
		Work: func(_ context.Context, result models.ForecastResult) (models.ForecastResult, error) {
			return result, nil
		},
	})

	finalize := func(_ context.Context, _ models.ForecastResult) error {
		log := zap.S().Named("forecast_service")

		if b.strategy != nil {
			teardownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := b.strategy.Teardown(teardownCtx); err != nil {
				log.Warnw("strategy teardown error", "pair", b.pair.Name, "error", err)
			}
		}

		if b.tempDir != "" && b.dc != nil {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := b.diskManager.DeleteDirectory(cleanCtx, b.dc, b.pair.SourceDatastore, b.tempDir); err != nil {
				log.Debugw("cleanup: failed to delete source dir", "error", err)
			}
			if b.pair.SourceDatastore != b.pair.TargetDatastore {
				if err := b.diskManager.DeleteDirectory(cleanCtx, b.dc, b.pair.TargetDatastore, b.tempDir); err != nil {
					log.Debugw("cleanup: failed to delete target dir", "error", err)
				}
			}
		}

		return nil
	}

	return work.NewSliceWorkBuilder2(units, finalize)
}
