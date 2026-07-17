package v2

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	rsig "github.com/kubev2v/assisted-migration-agent/pkg/rightsizing"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

const (
	rightsizingDefaultLookbackHours   = 720  // 30 days
	rightsizingDefaultIntervalSeconds = 7200 // monthly vSphere rollup
	rightsizingDefaultBatchSize       = 25
)

// Type aliases following the CollectorService pattern.
type (
	rightsizingWorkUnit = work.WorkUnit[models.RightsizingCollectionStatus, models.RightsizingCollectionResult]

	// RightsizingCollectionHandle bundles the work builder with a cleanup function
	// that logs out of vCenter when the pipeline finishes (success or error).
	// Exported so tests in package v2_test can construct fake handles.
	RightsizingCollectionHandle struct {
		Builder  work.WorkBuilder[models.RightsizingCollectionStatus, models.RightsizingCollectionResult]
		LogoutFn func()
	}

	// rightsizingWorkBuilderFunc builds the work pipeline for a single collection run.
	// Swappable via WithWorkBuilder for tests.
	rightsizingWorkBuilderFunc func(reportID string, cfg rsig.Config, discoverVMs bool, st *store.Store2, start, end time.Time) *RightsizingCollectionHandle
)

// rightsizingWorkBuilder is a custom WorkBuilder that emits three static stages
// (connect, discover, query) then dynamically generates one WorkUnit per batch
// after the query stage populates shared closure state.
type rightsizingWorkBuilder struct {
	staticUnits  []rightsizingWorkUnit
	staticIdx    int
	batchesReady bool
	batchUnits   []rightsizingWorkUnit
	batchIdx     int

	// Populated by static unit closures; read by generateBatches.
	vms       *[]rsig.VMInfo
	vmResults *map[string]rsig.VMReport
	batchSize int
	reportID  string
	store     *store.Store2
}

func (b *rightsizingWorkBuilder) Next() (rightsizingWorkUnit, bool) {
	// Drain static units first (connect, discover, query).
	if b.staticIdx < len(b.staticUnits) {
		u := b.staticUnits[b.staticIdx]
		b.staticIdx++
		return u, true
	}

	// Generate batch units once, after the query stage has populated vmResults.
	if !b.batchesReady {
		b.batchesReady = true
		b.generateBatches()
	}

	if b.batchIdx < len(b.batchUnits) {
		u := b.batchUnits[b.batchIdx]
		b.batchIdx++
		return u, true
	}

	return rightsizingWorkUnit{}, false
}

// generateBatches reads the populated closure state and creates one WorkUnit per batch.
// Called after the query stage completes, so vms and vmResults are available.
func (b *rightsizingWorkBuilder) generateBatches() {
	vms := *b.vms
	vmResults := *b.vmResults
	totalBatches := int(math.Ceil(float64(len(vms)) / float64(b.batchSize)))

	for i := 0; i < len(vms); i += b.batchSize {
		// Capture loop variables by value to avoid closure aliasing.
		batchNum := i/b.batchSize + 1
		capturedTotal := totalBatches
		batchVMs := vms[i:min(i+b.batchSize, len(vms))]
		metrics := toRightSizingStoreMetrics(batchVMs, vmResults)
		reportID := b.reportID
		st := b.store

		b.batchUnits = append(b.batchUnits, rightsizingWorkUnit{
			Status: func() models.RightsizingCollectionStatus {
				return models.RightsizingCollectionStatus{
					State:        models.RightsizingCollectionStatePersisting,
					BatchNum:     batchNum,
					TotalBatches: capturedTotal,
				}
			},
			Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
				if err := st.WithTx(ctx, func(txCtx context.Context) error {
					if err := st.RightSizing().WriteBatch(txCtx, reportID, metrics); err != nil {
						return err
					}
					return st.RightSizing().IncrementWrittenBatchCount(txCtx, reportID)
				}); err != nil {
					return result, fmt.Errorf("persisting batch %d/%d: %w", batchNum, capturedTotal, err)
				}
				return result, nil
			},
		})
	}

	// Collect VMs that had no metrics data and append a single work unit to persist their warnings.
	var noDataWarnings []models.VMWarning
	for _, vm := range vms {
		r := vmResults[vm.Ref.Value]
		if len(r.Metrics) == 0 {
			warning := "vCenter returned no data for this VM"
			if len(r.Warnings) > 0 {
				warning = r.Warnings[0]
			}
			noDataWarnings = append(noDataWarnings, models.VMWarning{
				MOID:    vm.Ref.Value,
				VMName:  vm.Name,
				Warning: warning,
			})
		}
	}

	if len(noDataWarnings) > 0 {
		capturedWarnings := noDataWarnings
		reportID := b.reportID
		st := b.store
		b.batchUnits = append(b.batchUnits, rightsizingWorkUnit{
			Status: func() models.RightsizingCollectionStatus {
				return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStatePersisting}
			},
			Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
				if err := st.RightSizing().WriteVMWarnings(ctx, reportID, capturedWarnings); err != nil {
					return result, fmt.Errorf("persisting VM warnings: %w", err)
				}
				return result, nil
			},
		})
	}

	// Final stage: compute and persist per-VM utilization percentages.
	// Runs after all metric batches and warnings are persisted.
	reportID := b.reportID
	st := b.store
	b.batchUnits = append(b.batchUnits, rightsizingWorkUnit{
		Status: func() models.RightsizingCollectionStatus {
			return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStatePersisting}
		},
		Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
			if err := st.RightSizing().ComputeAndStoreUtilization(ctx, reportID); err != nil {
				return result, fmt.Errorf("computing VM utilization: %w", err)
			}
			return result, nil
		},
	})
}

// RightsizingService provides API access to stored rightsizing reports and
// manages a single async collection run at a time.
type RightsizingService struct {
	mu      sync.Mutex
	store   *store.Store2
	buildFn rightsizingWorkBuilderFunc
	workSrv *work.Service[models.RightsizingCollectionStatus, models.RightsizingCollectionResult]
}

func NewRightsizingService(st *store.Store2) *RightsizingService {
	return &RightsizingService{
		store:   st,
		buildFn: defaultRightsizingWorkBuilder,
	}
}

// WithWorkBuilder replaces the work builder function. Used in tests to inject a fake pipeline.
func (s *RightsizingService) WithWorkBuilder(fn rightsizingWorkBuilderFunc) *RightsizingService {
	s.buildFn = fn
	return s
}

// ListReports returns metadata for all rightsizing reports (no VM metrics).
func (s *RightsizingService) ListReports(ctx context.Context) ([]models.RightsizingReportSummary, error) {
	return s.store.RightSizing().ListReports(ctx)
}

// GetReport returns a single rightsizing report by ID with full VM metrics.
func (s *RightsizingService) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	return s.store.RightSizing().GetReport(ctx, id)
}

// GetVMUtilization returns the full rightsizing utilization breakdown for a VM.
func (s *RightsizingService) GetVMUtilization(ctx context.Context, vmID string) (*models.VmUtilizationDetails, error) {
	return s.store.RightSizing().GetVMUtilization(ctx, vmID)
}

// ListClusterUtilization returns weighted cluster utilization for a specific report.
// filterExpr is an optional filter DSL expression (e.g. "cluster_id = 'domain-c123'"); empty means no filter.
func (s *RightsizingService) ListClusterUtilization(ctx context.Context, reportID, filterExpr string) ([]models.RightsizingClusterUtilization, error) {
	return s.store.RightSizing().ListClusterUtilization(ctx, reportID, filterExpr)
}

// ListLatestClusterUtilization returns weighted cluster utilization for the latest completed report.
// filterExpr is an optional filter DSL expression (e.g. "cluster_id = 'domain-c123'"); empty means no filter.
func (s *RightsizingService) ListLatestClusterUtilization(ctx context.Context, filterExpr string) (string, []models.RightsizingClusterUtilization, error) {
	return s.store.RightSizing().ListLatestClusterUtilization(ctx, filterExpr)
}

// applyCollectionDefaults fills zero-value params with package defaults.
func applyCollectionDefaults(p *models.RightsizingParams) {
	if p.LookbackH <= 0 {
		p.LookbackH = rightsizingDefaultLookbackHours
	}
	if p.IntervalID <= 0 {
		p.IntervalID = rightsizingDefaultIntervalSeconds
	}
	if p.BatchSize <= 0 {
		p.BatchSize = rightsizingDefaultBatchSize
	}
}

// computeCollectionWindow derives the time window and expected sample count from params.
func computeCollectionWindow(params models.RightsizingParams) (windowStart, windowEnd time.Time, expectedSamples int, lookback time.Duration) {
	windowEnd = time.Now().UTC()
	lookback = time.Duration(params.LookbackH) * time.Hour
	windowStart = windowEnd.Add(-lookback)
	expectedSamples = int(lookback / (time.Duration(params.IntervalID) * time.Second))
	return
}

// buildVSphereConfig constructs the rsig.Config from collection params.
func buildVSphereConfig(params models.RightsizingParams, lookback time.Duration) rsig.Config {
	return rsig.Config{
		VCenterURL: params.URL,
		Username:   params.Username,
		Password:   params.Password,
		Insecure:   params.SkipTLS,
		CACert:     params.CACert,
		NameFilter: params.NameFilter,
		ClusterID:  params.ClusterID,
		Lookback:   lookback,
		IntervalID: params.IntervalID,
		BatchSize:  params.BatchSize,
	}
}

// TriggerCollection starts an async rightsizing collection run.
// The report shell is persisted in DuckDB synchronously before returning (202 Accepted).
// Callers poll GET /rightsizing/{id} to observe metrics being populated.
func (s *RightsizingService) TriggerCollection(ctx context.Context, params models.RightsizingParams) (*models.RightsizingReportSummary, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	applyCollectionDefaults(&params)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.workSrv != nil && s.workSrv.IsRunning() {
		return nil, srvErrors.NewRightsizingCollectionInProgressError()
	}

	windowStart, windowEnd, expectedSamples, lookback := computeCollectionWindow(params)

	// Persist a shell report before launching the pipeline so the ID exists immediately.
	// vmCount=0 here; UpdateExpectedBatchCount corrects it after VM discovery.
	storeReport := models.RightSizingReport{
		VCenter:             params.URL,
		ClusterID:           params.ClusterID,
		IntervalID:          params.IntervalID,
		WindowStart:         windowStart,
		WindowEnd:           windowEnd,
		ExpectedSampleCount: expectedSamples,
	}
	reportID, createdAt, err := s.store.RightSizing().CreateReport(ctx, storeReport, 0, params.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("creating report shell: %w", err)
	}

	cfg := buildVSphereConfig(params, lookback)
	handle := s.buildFn(reportID, cfg, params.DiscoverVMs, s.store, windowStart, windowEnd)
	srv := work.NewService(
		models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateConnecting},
		handle.Builder,
	)
	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("starting collection pipeline: %w", err)
	}

	// Monitor goroutine: logout of vCenter when the pipeline finishes.
	go func() {
		for srv.IsRunning() {
			time.Sleep(500 * time.Millisecond)
		}
		handle.LogoutFn()

		state := srv.State()
		if state.Err != nil && !errors.Is(state.Err, work.ErrStopped) {
			zap.S().Named("rightsizing_service").Errorw("collection failed",
				"report_id", reportID, "error", state.Err)
		} else {
			zap.S().Named("rightsizing_service").Infow("collection completed",
				"report_id", reportID)
		}
	}()

	s.workSrv = srv

	return &models.RightsizingReportSummary{
		ID:                  reportID,
		VCenter:             params.URL,
		ClusterID:           params.ClusterID,
		WindowStart:         windowStart,
		WindowEnd:           windowEnd,
		IntervalID:          params.IntervalID,
		ExpectedSampleCount: expectedSamples,
		CreatedAt:           createdAt,
	}, nil
}

// GetStatus returns the current state of the async collection pipeline.
func (s *RightsizingService) GetStatus() models.RightsizingCollectionStatus {
	s.mu.Lock()
	srv := s.workSrv
	s.mu.Unlock()

	if srv == nil {
		return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateIdle}
	}
	state := srv.State()
	if state.Err != nil && !errors.Is(state.Err, work.ErrStopped) {
		return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateError, Error: state.Err}
	}
	// Pipeline finished with no error — the last unit's status stays in state.State
	// (e.g. "persisting") rather than transitioning to "completed" automatically.
	// Check IsRunning() to detect clean completion regardless of the final unit's label.
	if !srv.IsRunning() {
		return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateCompleted}
	}
	return state.State
}

// Stop cancels a running collection pipeline.
func (s *RightsizingService) Stop() {
	s.mu.Lock()
	srv := s.workSrv
	s.mu.Unlock()
	if srv != nil {
		srv.Stop()
	}
}

// defaultRightsizingWorkBuilder constructs the real four-stage govmomi pipeline.
// Stages: connect → discover → query → [batch-1 … batch-N] (dynamic).
func defaultRightsizingWorkBuilder(reportID string, cfg rsig.Config, discoverVMs bool, st *store.Store2, start, end time.Time) *RightsizingCollectionHandle {
	var client *govmomi.Client
	var vms []rsig.VMInfo
	var vmResults map[string]rsig.VMReport

	builder := &rightsizingWorkBuilder{
		vms:       &vms,
		vmResults: &vmResults,
		batchSize: cfg.BatchSize,
		reportID:  reportID,
		store:     st,
	}

	builder.staticUnits = []rightsizingWorkUnit{
		{
			Status: func() models.RightsizingCollectionStatus {
				return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateConnecting}
			},
			Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
				c, err := rsig.Connect(ctx, cfg)
				if err != nil {
					return result, fmt.Errorf("connecting to vCenter: %w", err)
				}
				client = c
				return result, nil
			},
		},
		{
			Status: func() models.RightsizingCollectionStatus {
				return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateDiscovering}
			},
			Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
				if discoverVMs {
					// Live vSphere discovery — original behaviour.
					discovered, err := rsig.DiscoverVMs(ctx, client, cfg)
					if err != nil {
						return result, fmt.Errorf("discovering VMs from vSphere: %w", err)
					}
					vms = discovered
				} else {
					// Inventory-based discovery from local DB (default).
					inventoryVMs, err := st.RightSizing().ListInventoryVMs(ctx)
					if err != nil {
						return result, fmt.Errorf("reading VMs from inventory: %w", err)
					}
					for _, vm := range inventoryVMs {
						vms = append(vms, rsig.VMInfo{
							Name: vm.Name,
							Ref: types.ManagedObjectReference{
								Type:  "VirtualMachine",
								Value: vm.ID,
							},
						})
					}
				}
				if err := st.RightSizing().UpdateExpectedBatchCount(ctx, reportID, len(vms), cfg.BatchSize); err != nil {
					return result, fmt.Errorf("updating expected batch count: %w", err)
				}
				return result, nil
			},
		},
		{
			Status: func() models.RightsizingCollectionStatus {
				return models.RightsizingCollectionStatus{State: models.RightsizingCollectionStateQuerying}
			},
			Work: func(ctx context.Context, result models.RightsizingCollectionResult) (models.RightsizingCollectionResult, error) {
				results, warnings := rsig.QueryMetrics(ctx, client, vms, cfg, start, end)
				if len(warnings) > 0 {
					zap.S().Named("rightsizing_service").Warnw("metric query warnings",
						"report_id", reportID, "warnings", warnings)
				}
				vmResults = results
				return result, nil
			},
		},
	}

	return &RightsizingCollectionHandle{
		Builder: builder,
		LogoutFn: func() {
			if client != nil {
				_ = client.Logout(context.Background())
			}
		},
	}
}

// BuildCollectorWorkUnits returns a postCollectionBuilderFn that can be passed
// to collectorWorkFactory.WithPostCollectionBuilder. When the collector pipeline
// finishes parsing inventory, the returned work unit launches a rightsizing run
// using the same vCenter credentials and blocks until it completes.
//
// DiscoverVMs is always false — the point of this flow is to use the inventory
// just built by the collector rather than querying vSphere again.
func (s *RightsizingService) BuildCollectorWorkUnits(lookbackH, intervalID, batchSize int) postCollectionBuilderFn {
	return func(creds models.Credentials) []collectorWorkUnit {
		return []collectorWorkUnit{
			{
				Status: func() models.CollectorStatus {
					return models.CollectorStatus{State: models.CollectorStateRightsizingConnecting}
				},
				Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
					params := models.RightsizingParams{
						Credentials: creds,
						LookbackH:   lookbackH,
						IntervalID:  intervalID,
						BatchSize:   batchSize,
						DiscoverVMs: false,
					}
					if _, err := s.TriggerCollection(ctx, params); err != nil {
						return r, fmt.Errorf("auto rightsizing trigger: %w", err)
					}
					for {
						select {
						case <-ctx.Done():
							return r, ctx.Err()
						default:
						}
						status := s.GetStatus()
						switch status.State {
						case models.RightsizingCollectionStateCompleted:
							return r, nil
						case models.RightsizingCollectionStateError:
							return r, status.Error
						}
						time.Sleep(500 * time.Millisecond)
					}
				},
			},
		}
	}
}

// toRightSizingStoreMetrics flattens per-VM metric maps for a batch into the
// flat []models.RightSizingMetric slice expected by WriteBatch.
func toRightSizingStoreMetrics(batchVMs []rsig.VMInfo, vmResults map[string]rsig.VMReport) []models.RightSizingMetric {
	var out []models.RightSizingMetric
	for _, vm := range batchVMs {
		r := vmResults[vm.Ref.Value]
		for key, stats := range r.Metrics {
			out = append(out, models.RightSizingMetric{
				VMName:      r.Name,
				MOID:        r.MOID,
				MetricKey:   key,
				SampleCount: stats.SampleCount,
				Average:     stats.Average,
				P95:         stats.P95,
				P99:         stats.P99,
				Max:         stats.Max,
				Latest:      stats.Latest,
			})
		}
	}
	return out
}
