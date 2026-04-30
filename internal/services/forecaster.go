package services

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
)

const (
	defaultForecastDiskSizeGB = 10
	defaultForecastIterations = 5
	maxForecastPairs          = 20
)

// ForecasterService orchestrates migration time estimation benchmarks between
// datastore pairs. It is a thin orchestrator: it creates a one-time consumable
// forecastService for each run and derives its state from whether that instance
// exists (nil = ready, non-nil = running).
type ForecasterService struct {
	mu          sync.Mutex
	forecastSvc *forecastService
	store       *store.Store
	pairLimit   int
	registry    *offload.Registry
	savedCreds  *models.Credentials // saved after successful inline credential verification (POST)
}

// NewForecasterService returns an idle forecaster.
func NewForecasterService(s *store.Store, pairLimit int) *ForecasterService {
	if pairLimit <= 0 {
		pairLimit = maxForecastPairs
	}
	return &ForecasterService{
		store:     s,
		pairLimit: pairLimit,
		registry:  offload.NewRegistry(),
	}
}

// GetStatus returns the current forecaster status including per-pair details.
func (f *ForecasterService) GetStatus() models.ForecasterStatus {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.forecastSvc == nil {
		return models.ForecasterStatus{State: models.ForecasterStateReady}
	}

	return models.ForecasterStatus{
		State: models.ForecasterStateRunning,
		Pairs: f.forecastSvc.GetPairStatuses(),
	}
}

// Start connects to vSphere and begins benchmarking the requested datastore pairs.
// Inline credentials are verified, saved, and used; if omitted, saved credentials are used.
func (f *ForecasterService) Start(ctx context.Context, req models.ForecastRequest) error {
	f.mu.Lock()

	if f.forecastSvc != nil {
		f.mu.Unlock()
		return srvErrors.NewForecasterInProgressError()
	}

	if len(req.Pairs) == 0 {
		f.mu.Unlock()
		return srvErrors.NewValidationError("at least one datastore pair is required")
	}

	if len(req.Pairs) > f.pairLimit {
		f.mu.Unlock()
		return srvErrors.NewForecasterLimitReachedError(f.pairLimit)
	}

	// Apply defaults
	if req.DiskSizeGB <= 0 {
		req.DiskSizeGB = defaultForecastDiskSizeGB
	}
	if req.Iterations <= 0 {
		req.Iterations = defaultForecastIterations
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 1
	}

	f.mu.Unlock()

	cred, err := f.resolveCredentials(ctx, req.Credentials)
	if err != nil {
		return err
	}

	zap.S().Infow("starting forecaster", "pairs", len(req.Pairs), "diskSizeGB", req.DiskSizeGB, "iterations", req.Iterations, "concurrency", req.Concurrency)

	vClient, err := vmware.NewVsphereClient(ctx, cred.URL, cred.Username, cred.Password, true)
	if err != nil {
		zap.S().Named("forecaster_service").Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	zap.S().Named("forecaster_service").Info("vSphere connection established")

	dm := vmware.NewDiskManager(vClient)

	strategyFactory := func() BenchmarkStrategy {
		return newVMStrategy(dm, vClient)
	}

	f.mu.Lock()
	// Re-check after releasing and re-acquiring lock
	if f.forecastSvc != nil {
		f.mu.Unlock()
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = vClient.Logout(logoutCtx)
		return srvErrors.NewForecasterInProgressError()
	}

	svc := newForecastService(f.store)
	f.forecastSvc = svc
	f.mu.Unlock()

	if err := svc.Start(dm, strategyFactory, req, func() {
		// Cleanup: logout vSphere and nil the forecastSvc reference
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = vClient.Logout(logoutCtx)

		f.mu.Lock()
		f.forecastSvc = nil
		f.mu.Unlock()
	}); err != nil {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = vClient.Logout(logoutCtx)

		f.mu.Lock()
		f.forecastSvc = nil
		f.mu.Unlock()
		return err
	}

	return nil
}

// VerifyCredentials validates vCenter credentials and required privileges
// without saving them. Used as a preflight check (PUT /forecaster/credentials).
func (f *ForecasterService) VerifyCredentials(ctx context.Context, credentials models.Credentials) error {
	if err := vmware.VerifyCredentialsAndPrivileges(ctx, &credentials, models.ForecasterRequiredPrivileges, "forecaster"); err != nil {
		return err
	}

	zap.S().Named("forecaster_service").Info("credentials verified successfully")
	return nil
}

// resolveCredentials returns inline credentials if provided (after verifying
// privileges and saving them), otherwise falls back to previously verified
// saved credentials, or returns CredentialsNotSetError.
func (f *ForecasterService) resolveCredentials(ctx context.Context, creds models.Credentials) (models.Credentials, error) {
	if creds.URL != "" {
		if err := vmware.VerifyCredentialsAndPrivileges(ctx, &creds, models.ForecasterRequiredPrivileges, "forecaster"); err != nil {
			return models.Credentials{}, err
		}
		f.mu.Lock()
		saved := creds
		f.savedCreds = &saved
		f.mu.Unlock()
		return creds, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.savedCreds != nil {
		return *f.savedCreds, nil
	}
	return models.Credentials{}, srvErrors.NewCredentialsNotSetError()
}

// Stop requests cancellation of all pair benchmarks and waits for cleanup.
func (f *ForecasterService) Stop() error {
	f.mu.Lock()

	if f.forecastSvc == nil {
		f.mu.Unlock()
		return srvErrors.NewForecasterNotRunningError()
	}

	svc := f.forecastSvc
	f.mu.Unlock()

	svc.Stop() // blocks until cleanup finishes (which nils f.forecastSvc)

	return nil
}

// IsBusy reports whether a forecast is currently running.
func (f *ForecasterService) IsBusy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forecastSvc != nil
}

// StopPair cancels a single pair within the running forecast.
func (f *ForecasterService) StopPair(pairName string) error {
	f.mu.Lock()
	svc := f.forecastSvc
	f.mu.Unlock()

	if svc == nil {
		return srvErrors.NewForecasterNotRunningError()
	}

	if !svc.StopPair(pairName) {
		return srvErrors.NewResourceNotFoundError("pair", pairName)
	}

	return nil
}

// DeleteRun deletes a specific benchmark run by ID.
func (f *ForecasterService) DeleteRun(ctx context.Context, runID int64) error {
	return f.store.Forecast().DeleteRun(ctx, runID)
}

// ListRuns returns all benchmark runs, optionally filtered by pair name.
func (f *ForecasterService) ListRuns(ctx context.Context, pairName string) ([]models.BenchmarkRun, error) {
	return f.store.Forecast().ListRuns(ctx, pairName)
}

// GetStats computes statistics for a given pair from all stored runs.
// Only successful runs (no error, positive throughput) are included.
func (f *ForecasterService) GetStats(ctx context.Context, pairName string) (*models.ForecastStats, error) {
	runs, err := f.store.Forecast().ListRuns(ctx, pairName)
	if err != nil {
		return nil, err
	}
	return computeForecastStats(pairName, runs), nil
}

func computeForecastStats(pairName string, runs []models.BenchmarkRun) *models.ForecastStats {
	var successful []models.BenchmarkRun
	for _, r := range runs {
		if r.Error == "" && r.ThroughputMBps > 0 {
			successful = append(successful, r)
		}
	}

	if len(successful) == 0 {
		return &models.ForecastStats{PairName: pairName, SampleCount: 0}
	}

	throughputs := make([]float64, len(successful))
	for i, r := range successful {
		throughputs[i] = r.ThroughputMBps
	}
	sort.Float64s(throughputs)

	n := len(throughputs)
	stats := &models.ForecastStats{
		PairName:    pairName,
		SampleCount: n,
		MinMBps:     throughputs[0],
		MaxMBps:     throughputs[n-1],
		MeanMBps:    sliceMean(throughputs),
		MedianMBps:  slicePercentile(throughputs, 50),
	}

	stats.StdDevMBps = sliceStdDev(throughputs, stats.MeanMBps)

	// 95% confidence interval using t-distribution approximation
	if n >= 2 {
		tValue := 2.0
		margin := tValue * stats.StdDevMBps / math.Sqrt(float64(n))
		stats.CI95Lower = stats.MeanMBps - margin
		stats.CI95Upper = stats.MeanMBps + margin
		if stats.CI95Lower < 0 {
			stats.CI95Lower = 0
		}
	} else {
		stats.CI95Lower = stats.MeanMBps
		stats.CI95Upper = stats.MeanMBps
	}

	// Time estimates for 1TB (1048576 MB)
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

// slicePercentile returns the p-th percentile from sorted values (p is 0-100).
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

// ListDatastores returns all datastores from the inventory with vendor and
// array information derived from NAA device identifiers. No vSphere queries
// are made — all data comes from the forklift-collected inventory.
func (f *ForecasterService) ListDatastores(ctx context.Context, _ models.Credentials) ([]models.DatastoreDetail, error) {
	rows, err := f.store.Forecast().ListDatastoreDetails(ctx)
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

// parseNAADevices parses a JSON array string of NAA device identifiers.
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

// PairCapabilities computes offload capabilities for a set of datastore pairs
// based on vendor profiles and storage array relationships derived from inventory.
func (f *ForecasterService) PairCapabilities(ctx context.Context, _ models.Credentials, req models.PairCapabilityRequest) ([]models.PairCapability, error) {
	datastores, err := f.ListDatastores(ctx, models.Credentials{})
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

func capStrings(caps []offload.Capability) []string {
	s := make([]string, len(caps))
	for i, c := range caps {
		s[i] = string(c)
	}
	return s
}
