package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/forecaster"
	"github.com/kubev2v/assisted-migration-agent/pkg/offload"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

const (
	defaultForecastDiskSizeGB = 10
	defaultForecastIterations = 5
	maxForecastPairs          = 20
)

// ForecasterService orchestrates migration time estimation benchmarks between
// datastore pairs. See doc.go for full documentation.
type ForecasterService struct {
	mu         sync.Mutex
	pool       *work.Pool2[models.ForecastPairStatus, models.ForecastResult]
	store      *store.Store
	pairLimit  int
	registry   *offload.Registry
	savedCreds *models.Credentials
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

	if f.pool == nil {
		return models.ForecasterStatus{State: models.ForecasterStateReady}
	}

	var pairs []models.ForecastPairStatus
	for _, s := range f.pool.All() {
		status := s.State
		if s.Err != nil {
			status.State = models.ForecastPairStateError
			status.Error = s.Err
		}
		pairs = append(pairs, status)
	}

	return models.ForecasterStatus{
		State: models.ForecasterStateRunning,
		Pairs: pairs,
	}
}

// Start connects to vSphere and begins benchmarking the requested datastore pairs.
// Inline credentials are verified, saved, and used; if omitted, saved credentials are used.
func (f *ForecasterService) Start(ctx context.Context, req models.ForecastRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.pool != nil {
		return srvErrors.NewForecasterInProgressError()
	}

	if len(req.Pairs) == 0 {
		return srvErrors.NewValidationError("at least one datastore pair is required")
	}

	if len(req.Pairs) > f.pairLimit {
		return srvErrors.NewForecasterLimitReachedError(f.pairLimit)
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

	if err := f.VerifyCredentials(ctx, req.Credentials); err != nil {
		return err
	}

	saved := req.Credentials
	f.savedCreds = &saved

	zap.S().Infow("starting forecaster", "pairs", len(req.Pairs), "diskSizeGB", req.DiskSizeGB, "iterations", req.Iterations, "concurrency", req.Concurrency)

	vClient, err := vmware.NewVsphereClient(ctx, req.Credentials.URL, req.Credentials.Username, req.Credentials.Password, true)
	if err != nil {
		zap.S().Named("forecaster_service").Errorw("failed to connect to vSphere", "error", err)
		return srvErrors.NewVCenterError(err)
	}

	zap.S().Named("forecaster_service").Info("vSphere connection established")

	dm := vmware.NewDiskManager(vClient)

	sessionID, err := f.store.Forecast().NextSessionID(ctx)
	if err != nil {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = vClient.Logout(logoutCtx)
		return fmt.Errorf("failed to allocate session ID: %w", err)
	}

	builders := make(map[string]work.WorkBuilder2[models.ForecastPairStatus, models.ForecastResult], len(req.Pairs))
	for _, pair := range req.Pairs {
		b := &forecastBuilder{
			diskManager: dm,
			store:       f.store,
			strategy:    forecaster.NewVMStrategy(dm, vClient),
			pair:        pair,
			diskSizeGB:  req.DiskSizeGB,
			iterations:  req.Iterations,
			sessionID:   sessionID,
		}
		builders[pair.Name] = b.New()
	}

	pool := work.NewPool2(builders).
		WithWorkers(req.Concurrency, len(req.Pairs)).
		WithFinalizer(func(_ context.Context) error {
			logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = vClient.Logout(logoutCtx)

			if f.mu.TryLock() {
				f.pool = nil
				f.mu.Unlock()
			}
			return nil
		})

	if err := pool.Start(); err != nil {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = vClient.Logout(logoutCtx)
		return err
	}

	f.pool = pool

	return nil
}

// VerifyCredentials validates vCenter credentials and required privileges.
func (f *ForecasterService) VerifyCredentials(ctx context.Context, creds models.Credentials) error {
	u, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return err
	}
	creds.URL = u

	parsedURL, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return err
	}
	parsedURL.User = url.UserPassword(creds.Username, creds.Password)

	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	vimClient, err := vim25.NewClient(verifyCtx, soap.NewClient(parsedURL, true))
	if err != nil {
		return err
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}

	log := zap.S().Named("forecaster_service")
	log.Info("verifying vCenter credentials")
	if err := client.Login(verifyCtx, parsedURL.User); err != nil {
		return srvErrors.NewVCenterError(err)
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
		client.CloseIdleConnections()
	}()

	log.Info("vCenter credentials verified, checking privileges")

	// Check privileges on the default VM folder (under the datacenter), since
	// vSphere privileges are typically granted at this level rather than root.
	finder := find.NewFinder(vimClient, true)
	dc, err := finder.DefaultDatacenter(verifyCtx)
	if err != nil {
		return srvErrors.NewVCenterError(fmt.Errorf("failed to find datacenter: %w", err))
	}
	finder.SetDatacenter(dc)
	vmFolder, err := finder.DefaultFolder(verifyCtx)
	if err != nil {
		return srvErrors.NewVCenterError(fmt.Errorf("failed to find VM folder: %w", err))
	}

	if err := vmware.ValidateUserPrivilegesOnEntity(verifyCtx, vimClient, vmFolder.Reference(), models.ForecasterRequiredPrivileges, creds.Username); err != nil {
		return err
	}

	log.Info("vCenter credentials and privileges verified successfully")
	return nil
}

// Stop requests cancellation of all pair benchmarks and waits for cleanup.
func (f *ForecasterService) Stop() error {
	f.mu.Lock()
	pool := f.pool
	f.mu.Unlock()

	if pool == nil {
		return srvErrors.NewForecasterNotRunningError()
	}

	_ = pool.Stop()
	return nil
}

// IsBusy reports whether a forecast is currently running.
func (f *ForecasterService) IsBusy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pool != nil
}

// StopPair cancels a single pair within the running forecast.
func (f *ForecasterService) StopPair(pairName string) error {
	f.mu.Lock()
	pool := f.pool
	f.mu.Unlock()

	if pool == nil {
		return srvErrors.NewForecasterNotRunningError()
	}

	status := pool.Cancel(pairName)
	if status.Err == nil && status.State.State == "" {
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
