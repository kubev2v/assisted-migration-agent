package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vmware/govmomi/object"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

const (
	defaultForecastSchedulerNormalWorkers   = 3
	defaultForecastSchedulerReservedWorkers = 0
)

type (
	forecastPipeline    = work.Pipeline[models.ForecastPairStatus, models.ForecastResult]
	forecastWorkBuilder func(pair models.DatastorePair, diskSizeGB, iterations int, sessionID int64) []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]
)

// forecastService is a one-time consumable process that owns a single forecast run.
// It manages per-pair WorkPipelines, the polling loop, and cleanup.
type forecastService struct {
	scheduler       *scheduler.Scheduler[models.ForecastResult]
	buildFn         forecastWorkBuilder
	pipelines       map[string]*forecastPipeline
	liveStatuses    map[string]*models.ForecastPairStatus // updated by runBenchmarks
	diskManager     *vmware.DiskManager
	strategyFactory func() BenchmarkStrategy // factory so each pipeline gets its own instance
	mu              sync.Mutex
	store           *store.Store
	stop            chan struct{}
	done            chan struct{} // closed when run() has fully finished (including cleanup)
	cleanUpFn       func()
}

func newForecastService(s *store.Store) *forecastService {
	return &forecastService{
		pipelines:    make(map[string]*forecastPipeline),
		liveStatuses: make(map[string]*models.ForecastPairStatus),
		store:        s,
	}
}

// Start creates the scheduler, pipelines, and launches the run loop.
func (f *forecastService) Start(dm *vmware.DiskManager, strategyFactory func() BenchmarkStrategy, req models.ForecastRequest, cleanupFn func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.diskManager = dm
	f.strategyFactory = strategyFactory
	f.stop = make(chan struct{}, 1)
	f.done = make(chan struct{})
	f.pipelines = make(map[string]*forecastPipeline)
	f.liveStatuses = make(map[string]*models.ForecastPairStatus)
	f.cleanUpFn = cleanupFn

	workers := req.Concurrency
	if workers <= 0 {
		workers = 1
	}
	sched, err := scheduler.NewScheduler[models.ForecastResult](workers, defaultForecastSchedulerReservedWorkers)
	if err != nil {
		return err
	}
	f.scheduler = sched

	if f.buildFn == nil {
		f.buildFn = f.buildForecastWorkUnits
	}

	// Allocate a session ID for this run
	sessionID, err := f.store.Forecast().NextSessionID(context.Background())
	if err != nil {
		return fmt.Errorf("failed to allocate session ID: %w", err)
	}

	zap.S().Named("forecast_service").Infow("starting forecast pipelines",
		"pairs", len(req.Pairs), "sessionID", sessionID)

	for _, pair := range req.Pairs {
		initialStatus := models.ForecastPairStatus{
			State:           models.ForecastPairStatePending,
			PairName:        pair.Name,
			SourceDatastore: pair.SourceDatastore,
			TargetDatastore: pair.TargetDatastore,
			Host:            pair.Host,
			TotalRuns:       req.Iterations,
		}

		pipeline := work.NewPipeline(initialStatus, f.scheduler,
			work.NewSliceWorkBuilder(f.buildFn(pair, req.DiskSizeGB, req.Iterations, sessionID)))
		if err := pipeline.Start(); err != nil {
			zap.S().Named("forecast_service").Errorw("failed to start pipeline", "pair", pair.Name, "error", err)
		}

		f.pipelines[pair.Name] = pipeline
	}

	go f.run()

	return nil
}

// Stop stops all pipelines, signals the run loop, and waits for full cleanup.
func (f *forecastService) Stop() {
	f.mu.Lock()
	if f.done == nil {
		f.mu.Unlock()
		return
	}

	pipelines := f.pipelines
	doneCh := f.done
	f.mu.Unlock()

	for _, pipeline := range pipelines {
		if pipeline != nil && pipeline.IsRunning() {
			pipeline.Stop()
		}
	}

	// Signal the run loop to exit (non-blocking: it may have already exited)
	select {
	case f.stop <- struct{}{}:
	default:
	}

	// Wait for run() to finish cleanup (close done channel)
	<-doneCh
}

// StopPair cancels a single pair's pipeline. Returns true if the pair was found
// and running, false if not found or already finished.
func (f *forecastService) StopPair(pairName string) bool {
	f.mu.Lock()
	pipeline, ok := f.pipelines[pairName]
	f.mu.Unlock()

	if !ok || pipeline == nil || !pipeline.IsRunning() {
		return false
	}

	pipeline.Stop()

	// Update live status to canceled
	f.mu.Lock()
	if s, ok := f.liveStatuses[pairName]; ok {
		s.State = models.ForecastPairStateCanceled
	} else {
		f.liveStatuses[pairName] = &models.ForecastPairStatus{
			State:    models.ForecastPairStateCanceled,
			PairName: pairName,
		}
	}
	f.mu.Unlock()

	return true
}

// run polls until all pipelines finish, then triggers cleanup.
func (f *forecastService) run() {
	ticker := time.NewTicker(5 * time.Second)

	defer func() {
		ticker.Stop()
		if f.scheduler != nil {
			f.scheduler.Close()
		}
		if f.cleanUpFn != nil {
			f.cleanUpFn()
		}
		close(f.done)
	}()

	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
			if !f.isBusy() {
				return
			}
		}
	}
}

// isBusy reports whether any pipeline is still running.
func (f *forecastService) isBusy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, p := range f.pipelines {
		if p.IsRunning() {
			return true
		}
	}
	return false
}

// GetPairStatuses returns the current status of all pair pipelines.
// Live statuses (updated in real time by runBenchmarks) take precedence
// over pipeline state, except when the pipeline has errored.
func (f *forecastService) GetPairStatuses() []models.ForecastPairStatus {
	f.mu.Lock()
	defer f.mu.Unlock()

	var statuses []models.ForecastPairStatus
	for name, p := range f.pipelines {
		state := p.State()

		// If the pipeline errored, report that directly
		if state.Err != nil {
			status := state.State
			status.State = models.ForecastPairStateError
			status.Error = state.Err
			statuses = append(statuses, status)
			continue
		}

		// Prefer live status if available (has real-time progress)
		if live, ok := f.liveStatuses[name]; ok {
			statuses = append(statuses, *live)
		} else {
			statuses = append(statuses, state.State)
		}
	}

	return statuses
}

// buildForecastWorkUnits creates the pipeline work units for benchmarking one pair.
// Each pipeline gets its own strategy instance to avoid concurrent state corruption.
func (f *forecastService) buildForecastWorkUnits(pair models.DatastorePair, diskSizeGB, iterations int, sessionID int64) []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult] {
	strategy := f.strategyFactory()

	return []work.WorkUnit[models.ForecastPairStatus, models.ForecastResult]{
		// Step 1: Validate datastores
		{
			Status: func() models.ForecastPairStatus {
				return models.ForecastPairStatus{
					State:           models.ForecastPairStateRunning,
					PairName:        pair.Name,
					SourceDatastore: pair.SourceDatastore,
					TargetDatastore: pair.TargetDatastore,
					Host:            pair.Host,
					TotalRuns:       iterations,
				}
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				return result, f.validateDatastores(ctx, pair)
			},
		},
		// Step 2: Run benchmark iterations
		{
			Status: func() models.ForecastPairStatus {
				return models.ForecastPairStatus{
					State:           models.ForecastPairStateRunning,
					PairName:        pair.Name,
					SourceDatastore: pair.SourceDatastore,
					TargetDatastore: pair.TargetDatastore,
					Host:            pair.Host,
					TotalRuns:       iterations,
				}
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				return f.runBenchmarks(ctx, strategy, pair, diskSizeGB, iterations, sessionID)
			},
		},
		// Step 3: Mark completed
		{
			Status: func() models.ForecastPairStatus {
				return models.ForecastPairStatus{
					State:           models.ForecastPairStateCompleted,
					PairName:        pair.Name,
					SourceDatastore: pair.SourceDatastore,
					TargetDatastore: pair.TargetDatastore,
					Host:            pair.Host,
					TotalRuns:       iterations,
					CompletedRuns:   iterations,
				}
			},
			Work: func(ctx context.Context, result models.ForecastResult) (models.ForecastResult, error) {
				return result, nil
			},
		},
	}
}

func (f *forecastService) validateDatastores(ctx context.Context, pair models.DatastorePair) error {
	log := zap.S().Named("forecast_service")
	log.Infow("validating datastores", "pair", pair.Name,
		"source", pair.SourceDatastore, "target", pair.TargetDatastore)

	dc, err := f.diskManager.FindDatacenter(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to find datacenter: %w", err)
	}

	if err := f.diskManager.DatastoreExists(ctx, dc, pair.SourceDatastore); err != nil {
		return fmt.Errorf("source datastore validation failed: %w", err)
	}

	if err := f.diskManager.DatastoreExists(ctx, dc, pair.TargetDatastore); err != nil {
		return fmt.Errorf("target datastore validation failed: %w", err)
	}

	log.Infow("datastores validated", "pair", pair.Name)
	return nil
}

// setLiveStatus updates the live status for a pair in a thread-safe way.
func (f *forecastService) setLiveStatus(pairName string, status models.ForecastPairStatus) {
	f.mu.Lock()
	s := status // copy
	f.liveStatuses[pairName] = &s
	f.mu.Unlock()
}

// updateLivePrepProgress atomically updates the prep bytes uploaded for a pair.
func (f *forecastService) updateLivePrepProgress(pairName string, bytesUploaded int64) {
	f.mu.Lock()
	if s, ok := f.liveStatuses[pairName]; ok {
		s.PrepBytesUploaded = bytesUploaded
	}
	f.mu.Unlock()
}

func (f *forecastService) runBenchmarks(ctx context.Context, strategy BenchmarkStrategy, pair models.DatastorePair, diskSizeGB, iterations int, sessionID int64) (models.ForecastResult, error) {
	log := zap.S().Named("forecast_service")
	var result models.ForecastResult

	dc, err := f.diskManager.FindDatacenter(ctx, "")
	if err != nil {
		return result, fmt.Errorf("failed to find datacenter: %w", err)
	}

	// Setup strategy for this pair (deploys filler image, etc.)
	if err := strategy.Setup(ctx, dc, pair); err != nil {
		return result, fmt.Errorf("strategy setup failed: %w", err)
	}
	selectedHost := strategy.SelectedHost()
	defer func() {
		teardownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := strategy.Teardown(teardownCtx); err != nil {
			log.Warnw("strategy teardown error", "pair", pair.Name, "error", err)
		}
	}()

	// ── Prep phase (once per pair): create disk + fill with random data ──
	totalPrepBytes := int64(diskSizeGB) * 1024 * 1024 * 1024

	f.setLiveStatus(pair.Name, models.ForecastPairStatus{
		State:             models.ForecastPairStatePreparing,
		PairName:          pair.Name,
		SourceDatastore:   pair.SourceDatastore,
		TargetDatastore:   pair.TargetDatastore,
		Host:              selectedHost,
		TotalRuns:         iterations,
		PrepBytesTotal:    totalPrepBytes,
		PrepBytesUploaded: 0,
	})

	prepStart := time.Now()

	tempDir := fmt.Sprintf("forecaster-%s-%d", pair.Name, time.Now().UnixNano())
	diskName := "benchmark-disk.vmdk"
	cloneName := "benchmark-disk-clone.vmdk"

	// Cleanup everything when done
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := f.diskManager.DeleteDirectory(cleanCtx, dc, pair.SourceDatastore, tempDir); err != nil {
			log.Debugw("cleanup: failed to delete source dir", "error", err)
		}
		if pair.SourceDatastore != pair.TargetDatastore {
			if err := f.diskManager.DeleteDirectory(cleanCtx, dc, pair.TargetDatastore, tempDir); err != nil {
				log.Debugw("cleanup: failed to delete target dir", "error", err)
			}
		}
	}()

	log.Infow("creating benchmark directories", "pair", pair.Name, "dir", tempDir)
	if err := f.diskManager.CreateDirectory(ctx, dc, pair.SourceDatastore, tempDir); err != nil {
		return result, fmt.Errorf("failed to create source directory: %w", err)
	}
	if pair.SourceDatastore != pair.TargetDatastore {
		if err := f.diskManager.CreateDirectory(ctx, dc, pair.TargetDatastore, tempDir); err != nil {
			return result, fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	log.Infow("creating benchmark disk", "pair", pair.Name, "sizeGB", diskSizeGB)
	if err := f.diskManager.CreateDisk(ctx, dc, pair.SourceDatastore, tempDir, diskName, diskSizeGB); err != nil {
		return result, fmt.Errorf("failed to create disk: %w", err)
	}

	srcPath := fmt.Sprintf("%s/%s", tempDir, diskName)

	log.Infow("filling disk with random data (once per pair)", "pair", pair.Name, "sizeGB", diskSizeGB)
	onProgress := func(bytesWritten int64) {
		f.updateLivePrepProgress(pair.Name, bytesWritten)
	}
	if err := strategy.FillDisk(ctx, dc, pair, srcPath, diskSizeGB, onProgress); err != nil {
		return result, fmt.Errorf("failed to fill disk: %w", err)
	}

	prepDuration := time.Since(prepStart)
	log.Infow("prep phase complete", "pair", pair.Name,
		"prepDuration", prepDuration.Round(time.Second))

	// ── Benchmark iterations: copy only ──
	dstPath := fmt.Sprintf("%s/%s", tempDir, cloneName)

	for i := 1; i <= iterations; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		// Update live status to running with completed count
		f.setLiveStatus(pair.Name, models.ForecastPairStatus{
			State:           models.ForecastPairStateRunning,
			PairName:        pair.Name,
			SourceDatastore: pair.SourceDatastore,
			TargetDatastore: pair.TargetDatastore,
			Host:            selectedHost,
			TotalRuns:       iterations,
			CompletedRuns:   i - 1,
		})

		log.Infow("benchmark iteration", "pair", pair.Name, "iteration", i, "of", iterations)

		run := f.runSingleBenchmark(ctx, strategy, dc, pair, srcPath, dstPath, diskSizeGB, i, sessionID)

		// Record prep time on the first iteration
		if i == 1 {
			run.PrepDurationSec = prepDuration.Seconds()
		}

		result.Runs = append(result.Runs, run)

		// Persist the run result
		if err := f.store.WithTx(ctx, func(txCtx context.Context) error {
			return f.store.Forecast().InsertRun(txCtx, run)
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
	}

	// Mark completed in live status
	f.setLiveStatus(pair.Name, models.ForecastPairStatus{
		State:           models.ForecastPairStateCompleted,
		PairName:        pair.Name,
		SourceDatastore: pair.SourceDatastore,
		TargetDatastore: pair.TargetDatastore,
		Host:            selectedHost,
		TotalRuns:       iterations,
		CompletedRuns:   iterations,
	})

	return result, nil
}

// runSingleBenchmark runs one copy iteration. The source disk is already
// created and filled — this only copies, measures, and deletes the clone.
func (f *forecastService) runSingleBenchmark(ctx context.Context, strategy BenchmarkStrategy, dc *object.Datacenter, pair models.DatastorePair, srcPath, dstPath string, diskSizeGB, iteration int, sessionID int64) models.BenchmarkRun {
	log := zap.S().Named("forecast_service")

	run := models.BenchmarkRun{
		SessionID:  sessionID,
		PairName:   pair.Name,
		SourceDS:   pair.SourceDatastore,
		TargetDS:   pair.TargetDatastore,
		Iteration:  iteration,
		DiskSizeGB: diskSizeGB,
		Method:     strategy.Name(),
	}

	benchResult, err := strategy.RunBenchmark(ctx, dc, pair, srcPath, dstPath, diskSizeGB)
	if err != nil {
		run.Error = fmt.Sprintf("benchmark failed: %v", err)
		run.DurationSec = benchResult.Duration.Seconds()
	} else {
		run.DurationSec = benchResult.Duration.Seconds()
		if run.DurationSec > 0 {
			run.ThroughputMBps = float64(diskSizeGB*1024) / run.DurationSec
		}
	}

	// Delete the clone so next iteration can reuse the same path
	cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if delErr := f.diskManager.DeleteDisk(cleanCtx, dc, pair.TargetDatastore, dstPath); delErr != nil {
		log.Debugw("cleanup: failed to delete clone disk", "path", dstPath, "error", delErr)
	}
	return run
}
