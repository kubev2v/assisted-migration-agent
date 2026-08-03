package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/offload"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

const (
	defaultForecastDiskSizeGB = 10
	defaultForecastIterations = 5
	maxForecastPairs          = 20
)

type ForecasterService struct {
	mu        sync.Mutex
	pool      *store.Pool
	workPool  *work.Pool2[models.ForecastPairStatus, models.ForecastResult]
	buildFn   forecastBuilderFactory
	pairNames []string
	registry  *offload.Registry
	credsSvc  *CredentialsService
}

func NewForecasterService(pool *store.Pool, credsSvc *CredentialsService) *ForecasterService {
	return &ForecasterService{
		pool:     pool,
		registry: offload.NewRegistry(),
		credsSvc: credsSvc,
	}
}

func (f *ForecasterService) GetStatus() models.ForecasterStatus {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.workPool == nil || !f.workPool.IsRunning() {
		return models.ForecasterStatus{State: models.ForecasterStateReady}
	}

	var pairs []models.ForecastPairStatus
	for _, name := range f.pairNames {
		s, err := f.workPool.State(name)
		if err != nil {
			continue
		}
		pairs = append(pairs, s)
	}

	return models.ForecasterStatus{
		State: models.ForecasterStateRunning,
		Pairs: pairs,
	}
}

func (f *ForecasterService) Start(ctx context.Context, req models.ForecastRequest) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.workPool != nil && f.workPool.IsRunning() {
		return srvErrors.NewForecasterInProgressError()
	}
	f.workPool = nil
	f.pairNames = nil

	if len(req.Pairs) == 0 {
		return srvErrors.NewValidationError("at least one datastore pair is required")
	}

	if len(req.Pairs) > maxForecastPairs {
		return srvErrors.NewForecasterLimitReachedError(maxForecastPairs)
	}

	if req.DiskSizeGB <= 0 {
		req.DiskSizeGB = defaultForecastDiskSizeGB
	}
	if req.Iterations <= 0 {
		req.Iterations = defaultForecastIterations
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 1
	}

	creds, err := f.credsSvc.Resolve(ctx)
	if err != nil {
		return err
	}

	url, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return err
	}
	creds.URL = url

	log := zap.S().Named("forecaster_service")
	log.Infow("starting forecaster", "pairs", len(req.Pairs), "diskSizeGB", req.DiskSizeGB, "iterations", req.Iterations, "concurrency", req.Concurrency)

	vClient, err := vmware.NewVsphereClient(ctx, &creds)
	if err != nil {
		log.Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	log.Info("vSphere connection established")

	defer func() {
		if err != nil {
			logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = vClient.Logout(logoutCtx)
		}
	}()

	dm := vmware.NewDiskManager(vClient)

	mainStore, err := f.mainStore()
	if err != nil {
		return fmt.Errorf("failed to access main database: %w", err)
	}

	sessionID, err := mainStore.Forecast().NextSessionID(ctx)
	if err != nil {
		return fmt.Errorf("failed to allocate session ID: %w", err)
	}

	buildFn := f.buildFn
	if buildFn == nil {
		buildFn = defaultForecastBuilderFactory(dm, mainStore, func() BenchmarkStrategy {
			return newVMStrategy(dm, vClient)
		}, req.DiskSizeGB, req.Iterations, sessionID)
	}

	builders := make(map[string]work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult], len(req.Pairs))
	pairNames := make([]string, 0, len(req.Pairs))
	for _, pair := range req.Pairs {
		builders[pair.Name] = buildFn(pair)
		pairNames = append(pairNames, pair.Name)
	}

	wp := work.NewPool2(builders).
		WithWorkers(req.Concurrency, len(req.Pairs)).
		WithFinalizer(func(_ context.Context) error {
			logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = vClient.Logout(logoutCtx)
			return nil
		})

	if err = wp.Start(); err != nil {
		return err
	}

	f.workPool = wp
	f.pairNames = pairNames

	return nil
}

func (f *ForecasterService) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	wp := f.workPool
	f.workPool = nil
	f.pairNames = nil

	if wp == nil {
		return nil
	}

	return wp.Stop()
}

func (f *ForecasterService) StopPair(pairName string) error {
	f.mu.Lock()
	wp := f.workPool
	f.mu.Unlock()

	if wp == nil {
		return srvErrors.NewForecasterNotRunningError()
	}

	if _, err := wp.Cancel(pairName); err != nil {
		return srvErrors.NewResourceNotFoundError("pair", pairName)
	}

	return nil
}

func (f *ForecasterService) DeleteRun(ctx context.Context, runID int64) error {
	st, err := f.mainStore()
	if err != nil {
		return err
	}
	return st.Forecast().DeleteRun(ctx, runID)
}

func (f *ForecasterService) ListRuns(ctx context.Context, pairName string) ([]models.BenchmarkRun, error) {
	st, err := f.mainStore()
	if err != nil {
		return nil, err
	}
	return st.Forecast().ListRuns(ctx, pairName)
}

func (f *ForecasterService) GetStats(ctx context.Context, pairName string) (models.ForecastStats, error) {
	st, err := f.mainStore()
	if err != nil {
		return models.ForecastStats{}, err
	}
	runs, err := st.Forecast().ListRuns(ctx, pairName)
	if err != nil {
		return models.ForecastStats{}, err
	}
	stats := computeForecastStats(pairName, runs)
	if stats.SampleCount == 0 {
		return models.ForecastStats{}, srvErrors.NewResourceNotFoundError("forecast stats", pairName)
	}
	return stats, nil
}

func (f *ForecasterService) ListDatastores(ctx context.Context) ([]models.DatastoreDetail, error) {
	st, err := f.latestCollectionStore()
	if err != nil {
		return nil, err
	}

	rows, err := st.Forecast().ListDatastoreDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list datastores from inventory: %w", err)
	}

	const mibPerGB = 1024.0
	result := make([]models.DatastoreDetail, 0, len(rows))
	for _, row := range rows {
		naaDevices := parseNAADevices(row.BackingDevices)

		vendor := ""
		if len(naaDevices) > 0 {
			vendor = offload.VendorFromNAA(naaDevices[0])
		}

		detail := models.DatastoreDetail{
			Name:           row.Name,
			Type:           row.Type,
			CapacityGB:     row.CapacityMiB / mibPerGB,
			FreeGB:         row.FreeMiB / mibPerGB,
			StorageVendor:  vendor,
			StorageArrayID: vmware.StorageArrayID(naaDevices),
			NAADevices:     naaDevices,
		}

		caps := f.registry.DatastoreCapabilities(vendor, detail.Type)
		if caps != nil {
			detail.Capabilities = capStrings(caps)
		}

		result = append(result, detail)
	}

	return result, nil
}

func (f *ForecasterService) PairCapabilities(ctx context.Context, req models.PairCapabilityRequest) ([]models.PairCapability, error) {
	datastores, err := f.ListDatastores(ctx)
	if err != nil {
		return nil, err
	}

	dsMap := make(map[string]models.DatastoreDetail, len(datastores))
	for _, ds := range datastores {
		dsMap[ds.Name] = ds
	}

	result := make([]models.PairCapability, 0, len(req.Pairs))
	for _, pair := range req.Pairs {
		src, srcOK := dsMap[pair.SourceDatastore]
		tgt, tgtOK := dsMap[pair.TargetDatastore]
		if !srcOK || !tgtOK {
			var missing []string
			if !srcOK {
				missing = append(missing, pair.SourceDatastore)
			}
			if !tgtOK {
				missing = append(missing, pair.TargetDatastore)
			}
			return nil, srvErrors.NewValidationError(fmt.Sprintf("datastore(s) not found: %v", missing))
		}

		caps := f.registry.PairCapabilities(
			src.StorageVendor, tgt.StorageVendor,
			src.StorageArrayID, tgt.StorageArrayID,
			tgt.Type,
		)

		pc := models.PairCapability{
			PairName:        pair.Name,
			SourceDatastore: pair.SourceDatastore,
			TargetDatastore: pair.TargetDatastore,
		}
		if caps != nil {
			pc.Capabilities = capStrings(caps)
		}
		result = append(result, pc)
	}

	return result, nil
}

func (f *ForecasterService) mainStore() (*store.Store2, error) {
	db, err := f.pool.Get(store.MainDatabaseID)
	if err != nil {
		return nil, err
	}
	return db.Store()
}

func (f *ForecasterService) latestCollectionStore() (*store.Store2, error) {
	db, err := f.pool.Latest()
	if err != nil {
		return nil, err
	}
	return db.Store()
}

func parseNAADevices(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}

	var devices []string
	if err := json.Unmarshal([]byte(raw), &devices); err != nil {
		return nil
	}
	return devices
}

func capStrings(caps []offload.Capability) []string {
	s := make([]string, len(caps))
	for i, c := range caps {
		s[i] = string(c)
	}
	return s
}

func computeForecastStats(pairName string, runs []models.BenchmarkRun) models.ForecastStats {
	var successful []models.BenchmarkRun
	for _, r := range runs {
		if r.Error == "" && r.ThroughputMBps > 0 {
			successful = append(successful, r)
		}
	}

	if len(successful) == 0 {
		return models.ForecastStats{PairName: pairName, SampleCount: 0}
	}

	throughputs := make([]float64, len(successful))
	for i, r := range successful {
		throughputs[i] = r.ThroughputMBps
	}
	sort.Float64s(throughputs)

	n := len(throughputs)
	stats := models.ForecastStats{
		PairName:    pairName,
		SampleCount: n,
		MinMBps:     throughputs[0],
		MaxMBps:     throughputs[n-1],
		MeanMBps:    sliceMean(throughputs),
		MedianMBps:  slicePercentile(throughputs, 50),
	}

	stats.StdDevMBps = sliceStdDev(throughputs, stats.MeanMBps)

	if n >= 2 {
		margin := tValue95(n-1) * stats.StdDevMBps / math.Sqrt(float64(n))
		stats.CI95Lower = stats.MeanMBps - margin
		stats.CI95Upper = stats.MeanMBps + margin
		if stats.CI95Lower < 0 {
			stats.CI95Lower = 0
		}
	} else {
		stats.CI95Lower = stats.MeanMBps
		stats.CI95Upper = stats.MeanMBps
	}

	const oneTBinMB = 1048576.0
	stats.EstPer1TB = models.EstimateRange{
		BestCase:  time.Duration(oneTBinMB / stats.MaxMBps * float64(time.Second)),
		Expected:  time.Duration(oneTBinMB / stats.MedianMBps * float64(time.Second)),
		WorstCase: time.Duration(oneTBinMB / stats.MinMBps * float64(time.Second)),
	}

	return stats
}

func sliceMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func sliceStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, v := range values {
		d := v - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
}

func slicePercentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	rank := (p / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

// Two-tailed t-distribution critical values at 95% confidence (α=0.05).
// https://www.medcalc.org/en/manual/t-distribution-table.php
func tValue95(df int) float64 {
	table := [...]float64{
		0,      // df=0 (unused)
		12.706, // df=1
		4.303,  // df=2
		3.182,  // df=3
		2.776,  // df=4
		2.571,  // df=5
		2.447,  // df=6
		2.365,  // df=7
		2.306,  // df=8
		2.262,  // df=9
		2.228,  // df=10
	}
	if df < len(table) {
		return table[df]
	}
	return 1.96
}
