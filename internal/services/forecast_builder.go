package services

import (
	"context"
	"fmt"
	"time"

	"github.com/vmware/govmomi/object"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type forecastRunCtx struct {
	svc        *forecastService
	strategy   BenchmarkStrategy
	pair       models.DatastorePair
	diskSizeGB int
	iterations int
	sessionID  int64

	dc           *object.Datacenter
	selectedHost string
	tempDir      string
	srcPath      string
	dstPath      string
	prepDuration time.Duration
}

type forecastBuilder struct {
	fctx  *forecastRunCtx
	units []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]
	idx   int
}

func buildForecastWork(fctx *forecastRunCtx) work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult] {
	pair := fctx.pair
	iterations := fctx.iterations
	diskSizeGB := fctx.diskSizeGB

	statusFor := func(state models.ForecastPairState) models.ForecastPairStatus {
		return models.ForecastPairStatus{
			State:           state,
			PairName:        pair.Name,
			SourceDatastore: pair.SourceDatastore,
			TargetDatastore: pair.TargetDatastore,
			Host:            fctx.selectedHost,
			TotalRuns:       iterations,
		}
	}

	units := []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
		// 1. Validate datastores — checks that both source and target datastores
		//    exist and are accessible. Status: Preparing.
		{
			Status: func() models.ForecastPairStatus { return statusFor(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				return result, fctx.svc.validateDatastores(ctx, pair)
			},
		},
		// 2. Strategy setup — finds the datacenter, deploys the filler VM via
		//    the strategy, and records the selected ESXi host. Status: Preparing.
		{
			Status: func() models.ForecastPairStatus { return statusFor(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				dc, err := fctx.svc.diskManager.FindDatacenter(ctx, "")
				if err != nil {
					return result, fmt.Errorf("failed to find datacenter: %w", err)
				}
				fctx.dc = dc
				if err := fctx.strategy.Setup(ctx, fctx.dc, pair); err != nil {
					return result, fmt.Errorf("strategy setup failed: %w", err)
				}
				fctx.selectedHost = fctx.strategy.SelectedHost()
				return result, nil
			},
		},
		// 3. Create directories — creates a temporary directory on the source
		//    datastore (and target if different) and sets srcPath/dstPath for
		//    subsequent steps. Status: Preparing.
		{
			Status: func() models.ForecastPairStatus { return statusFor(models.ForecastPairStatePreparing) },
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")
				fctx.tempDir = fmt.Sprintf("forecaster-%s-%d", pair.Name, time.Now().UnixNano())
				fctx.srcPath = fmt.Sprintf("%s/%s", fctx.tempDir, "benchmark-disk.vmdk")
				fctx.dstPath = fmt.Sprintf("%s/%s", fctx.tempDir, "benchmark-disk-clone.vmdk")

				log.Infow("creating benchmark directories", "pair", pair.Name, "dir", fctx.tempDir)
				if err := fctx.svc.diskManager.CreateDirectory(ctx, fctx.dc, pair.SourceDatastore, fctx.tempDir); err != nil {
					return result, fmt.Errorf("failed to create source directory: %w", err)
				}
				if pair.SourceDatastore != pair.TargetDatastore {
					if err := fctx.svc.diskManager.CreateDirectory(ctx, fctx.dc, pair.TargetDatastore, fctx.tempDir); err != nil {
						return result, fmt.Errorf("failed to create target directory: %w", err)
					}
				}
				return result, nil
			},
		},
		// 4. Create & fill disk — creates a thin-provisioned VMDK and fills it
		//    with random data to defeat zero-block optimization. Updates live
		//    status with prep byte progress. Status: Preparing (with PrepBytesTotal).
		{
			Status: func() models.ForecastPairStatus {
				s := statusFor(models.ForecastPairStatePreparing)
				s.PrepBytesTotal = int64(diskSizeGB) * 1024 * 1024 * 1024
				return s
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")

				totalPrepBytes := int64(diskSizeGB) * 1024 * 1024 * 1024
				fctx.svc.setLiveStatus(pair.Name, models.ForecastPairStatus{
					State:             models.ForecastPairStatePreparing,
					PairName:          pair.Name,
					SourceDatastore:   pair.SourceDatastore,
					TargetDatastore:   pair.TargetDatastore,
					Host:              fctx.selectedHost,
					TotalRuns:         iterations,
					PrepBytesTotal:    totalPrepBytes,
					PrepBytesUploaded: 0,
				})

				prepStart := time.Now()

				log.Infow("creating benchmark disk", "pair", pair.Name, "sizeGB", diskSizeGB)
				if err := fctx.svc.diskManager.CreateDisk(ctx, fctx.dc, pair.SourceDatastore, fctx.tempDir, "benchmark-disk.vmdk", diskSizeGB); err != nil {
					return result, fmt.Errorf("failed to create disk: %w", err)
				}

				log.Infow("filling disk with random data", "pair", pair.Name, "sizeGB", diskSizeGB)
				onProgress := func(bytesWritten int64) {
					fctx.svc.updateLivePrepProgress(pair.Name, bytesWritten)
				}
				if err := fctx.strategy.FillDisk(ctx, fctx.dc, pair, fctx.srcPath, diskSizeGB, onProgress); err != nil {
					return result, fmt.Errorf("failed to fill disk: %w", err)
				}

				fctx.prepDuration = time.Since(prepStart)
				log.Infow("prep phase complete", "pair", pair.Name,
					"prepDuration", fctx.prepDuration.Round(time.Second))

				return result, nil
			},
		},
	}

	// 5..5+N. Benchmark iterations — one work unit per iteration. Each copies
	//    the source disk to dstPath, measures throughput, persists the run to the
	//    store, and deletes the clone. Status: Running (with CompletedRuns).
	for iter := 1; iter <= iterations; iter++ {
		i := iter
		units = append(units, work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
			Status: func() models.ForecastPairStatus {
				s := statusFor(models.ForecastPairStateRunning)
				s.CompletedRuns = i - 1
				return s
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				log := zap.S().Named("forecast_service")

				fctx.svc.setLiveStatus(pair.Name, models.ForecastPairStatus{
					State:           models.ForecastPairStateRunning,
					PairName:        pair.Name,
					SourceDatastore: pair.SourceDatastore,
					TargetDatastore: pair.TargetDatastore,
					Host:            fctx.selectedHost,
					TotalRuns:       iterations,
					CompletedRuns:   i - 1,
				})

				log.Infow("benchmark iteration", "pair", pair.Name, "iteration", i, "of", iterations)

				run := fctx.svc.runSingleBenchmark(ctx, fctx.strategy, fctx.dc, pair,
					fctx.srcPath, fctx.dstPath, diskSizeGB, i, fctx.sessionID)

				if i == 1 {
					run.PrepDurationSec = fctx.prepDuration.Seconds()
				}

				result.Runs = append(result.Runs, run)

				if err := fctx.svc.store.WithTx(ctx, func(txCtx context.Context) error {
					return fctx.svc.store.Forecast().InsertRun(txCtx, run)
				}); err != nil {
					log.Errorw("failed to persist benchmark run", "pair", pair.Name, "iteration", i, "error", err)
				}

				if run.Error != "" {
					log.Infow("benchmark iteration failed", "pair", pair.Name, "iteration", i, "error", run.Error)
				} else {
					log.Infow("benchmark iteration complete", "pair", pair.Name,
						"iteration", i,
						"duration_sec", fmt.Sprintf("%.1f", run.DurationSec),
						"throughput_mbps", fmt.Sprintf("%.1f", run.ThroughputMBps))
				}

				return result, nil
			},
		})
	}

	// Last. Mark completed — no-op work unit that signals the pipeline finished
	//    successfully. Status: Completed (with CompletedRuns = iterations).
	units = append(units, work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
		Status: func() models.ForecastPairStatus {
			s := statusFor(models.ForecastPairStateCompleted)
			s.CompletedRuns = iterations
			return s
		},
		Work: func(_ context.Context, result models.ForecastResult) (models.ForecastResult, error) {
			fctx.svc.setLiveStatus(pair.Name, models.ForecastPairStatus{
				State:           models.ForecastPairStateCompleted,
				PairName:        pair.Name,
				SourceDatastore: pair.SourceDatastore,
				TargetDatastore: pair.TargetDatastore,
				Host:            fctx.selectedHost,
				TotalRuns:       iterations,
				CompletedRuns:   iterations,
			})
			return result, nil
		},
	})

	return &forecastBuilder{fctx: fctx, units: units}
}

func (b *forecastBuilder) Next() (work.WorkUnit[models.ForecastPairStatus, models.ForecastResult], bool) {
	if b.idx >= len(b.units) {
		return work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{}, false
	}
	u := b.units[b.idx]
	b.idx++
	return u, true
}

func (b *forecastBuilder) Finalize(_ context.Context, _ models.ForecastResult) error {
	log := zap.S().Named("forecast_service")
	fctx := b.fctx

	if fctx.strategy != nil {
		teardownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := fctx.strategy.Teardown(teardownCtx); err != nil {
			log.Warnw("strategy teardown error", "pair", fctx.pair.Name, "error", err)
		}
	}

	if fctx.tempDir != "" && fctx.dc != nil {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := fctx.svc.diskManager.DeleteDirectory(cleanCtx, fctx.dc, fctx.pair.SourceDatastore, fctx.tempDir); err != nil {
			log.Debugw("cleanup: failed to delete source dir", "error", err)
		}
		if fctx.pair.SourceDatastore != fctx.pair.TargetDatastore {
			if err := fctx.svc.diskManager.DeleteDirectory(cleanCtx, fctx.dc, fctx.pair.TargetDatastore, fctx.tempDir); err != nil {
				log.Debugw("cleanup: failed to delete target dir", "error", err)
			}
		}
	}

	return nil
}
