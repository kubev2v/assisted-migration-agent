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
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

// forecastService is a one-time consumable process that owns a single forecast run.
// It uses Pool2 to manage per-pair pipelines with automatic finalization.
type forecastService struct {
	pool            *work.Pool2[models.ForecastPairStatus, models.ForecastResult]
	pairNames       []string
	liveStatuses    map[string]*models.ForecastPairStatus
	diskManager     *vmware.DiskManager
	strategyFactory func() BenchmarkStrategy
	mu              sync.Mutex
	store           *store.Store
}

func newForecastService(s *store.Store) *forecastService {
	return &forecastService{
		liveStatuses: make(map[string]*models.ForecastPairStatus),
		store:        s,
	}
}

// Start creates Pool2 builders for each pair and starts the pool.
func (f *forecastService) Start(dm *vmware.DiskManager, strategyFactory func() BenchmarkStrategy, req models.ForecastRequest, cleanupFn func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.diskManager = dm
	f.strategyFactory = strategyFactory
	f.liveStatuses = make(map[string]*models.ForecastPairStatus)

	sessionID, err := f.store.Forecast().NextSessionID(context.Background())
	if err != nil {
		return fmt.Errorf("failed to allocate session ID: %w", err)
	}

	zap.S().Named("forecast_service").Infow("starting forecast pool",
		"pairs", len(req.Pairs), "sessionID", sessionID)

	builders := make(map[string]work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult], len(req.Pairs))
	f.pairNames = make([]string, 0, len(req.Pairs))

	for _, pair := range req.Pairs {
		fctx := &forecastRunCtx{
			svc:        f,
			strategy:   strategyFactory(),
			pair:       pair,
			diskSizeGB: req.DiskSizeGB,
			iterations: req.Iterations,
			sessionID:  sessionID,
		}
		builders[pair.Name] = buildForecastWork(fctx)
		f.pairNames = append(f.pairNames, pair.Name)
	}

	workers := req.Concurrency
	if workers <= 0 {
		workers = 1
	}

	pool := work.NewPool2(builders).
		WithWorkers(workers, len(req.Pairs)).
		WithFinalizer(func(_ context.Context) error {
			if cleanupFn != nil {
				cleanupFn()
			}
			return nil
		})

	if err := pool.Start(); err != nil {
		return err
	}

	f.pool = pool
	return nil
}

// Stop cancels all pairs and waits for finalization.
func (f *forecastService) Stop() {
	f.mu.Lock()
	pool := f.pool
	f.mu.Unlock()

	if pool == nil {
		return
	}

	_ = pool.Stop()
}

// StopPair cancels a single pair's pipeline.
func (f *forecastService) StopPair(pairName string) bool {
	f.mu.Lock()
	pool := f.pool
	f.mu.Unlock()

	if pool == nil {
		return false
	}

	status := pool.Cancel(pairName)

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

	return status.Err != nil || status.State.State != ""
}

// GetPairStatuses returns the current status of all pair pipelines.
func (f *forecastService) GetPairStatuses() []models.ForecastPairStatus {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.pool == nil {
		return nil
	}

	var statuses []models.ForecastPairStatus
	for _, name := range f.pairNames {
		state, err := f.pool.State(name)
		if err != nil {
			continue
		}

		if state.Err != nil {
			status := state.State
			status.State = models.ForecastPairStateError
			status.Error = state.Err
			statuses = append(statuses, status)
			continue
		}

		if live, ok := f.liveStatuses[name]; ok {
			statuses = append(statuses, *live)
		} else {
			statuses = append(statuses, state.State)
		}
	}

	return statuses
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

func (f *forecastService) setLiveStatus(pairName string, status models.ForecastPairStatus) {
	f.mu.Lock()
	s := status
	f.liveStatuses[pairName] = &s
	f.mu.Unlock()
}

func (f *forecastService) updateLivePrepProgress(pairName string, bytesUploaded int64) {
	f.mu.Lock()
	if s, ok := f.liveStatuses[pairName]; ok {
		s.PrepBytesUploaded = bytesUploaded
	}
	f.mu.Unlock()
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

	cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if delErr := f.diskManager.DeleteDisk(cleanCtx, dc, pair.TargetDatastore, dstPath); delErr != nil {
		log.Debugw("cleanup: failed to delete clone disk", "path", dstPath, "error", delErr)
	}
	return run
}
