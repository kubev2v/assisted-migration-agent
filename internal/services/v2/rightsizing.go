package v2

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

const (
	rightsizingDefaultLookbackHours   = 720  // 30 days
	rightsizingDefaultIntervalSeconds = 7200 // monthly vSphere rollup
	rightsizingDefaultBatchSize       = 25
)

var desiredMetrics = []string{
	"cpu.usagemhz.average",
	"cpu.usage.average",
	"mem.active.average",
	"mem.consumed.average",
	"disk.used.latest",
	"disk.provisioned.latest",
}

type VMInfo struct {
	Name string
	Ref  types.ManagedObjectReference
}

type VMReport struct {
	Name     string
	MOID     string
	Metrics  map[string]MetricStats
	Warnings []string
}

type MetricStats struct {
	SampleCount int     `json:"sample_count"`
	Average     float64 `json:"average"`
	P95         float64 `json:"p95"`
	P99         float64 `json:"p99"`
	Max         float64 `json:"max"`
	Latest      float64 `json:"latest"`
}

type RightsizingService struct {
	store *store.Store2
}

func NewRightsizingService(st *store.Store2) *RightsizingService {
	return &RightsizingService{store: st}
}

func (s *RightsizingService) GetVMUtilization(ctx context.Context, vmID string) (*models.VmUtilizationDetails, error) {
	return s.store.RightSizing().GetVMUtilization(ctx, vmID)
}

func (s *RightsizingService) ListLatestClusterUtilization(ctx context.Context, clusterID string) (string, []models.RightsizingClusterUtilization, error) {
	// TODO: after v1 removal fix this
	return s.store.RightSizing().ListLatestClusterUtilization(ctx, fmt.Sprintf("cluster_id = '%s'", clusterID))
}

func (s *RightsizingService) CreateReportFromInventory(ctx context.Context) (string, []VMInfo, time.Time, time.Time, error) {
	inventoryVMs, err := s.store.RightSizing().ListInventoryVMs(ctx)
	if err != nil {
		return "", nil, time.Time{}, time.Time{}, fmt.Errorf("reading VMs from inventory: %w", err)
	}

	var vms []VMInfo
	for _, vm := range inventoryVMs {
		vms = append(vms, VMInfo{
			Name: vm.Name,
			Ref:  types.ManagedObjectReference{Type: "VirtualMachine", Value: vm.ID},
		})
	}

	lookback := time.Duration(rightsizingDefaultLookbackHours) * time.Hour
	windowEnd := time.Now().UTC()
	windowStart := windowEnd.Add(-lookback)

	report := models.RightSizingReport{
		IntervalID:          rightsizingDefaultIntervalSeconds,
		WindowStart:         windowStart,
		WindowEnd:           windowEnd,
		ExpectedSampleCount: int(lookback / (time.Duration(rightsizingDefaultIntervalSeconds) * time.Second)),
	}
	id, _, err := s.store.RightSizing().CreateReport(ctx, report, len(vms), rightsizingDefaultBatchSize)
	if err != nil {
		return "", nil, time.Time{}, time.Time{}, fmt.Errorf("creating rightsizing report shell: %w", err)
	}
	return id, vms, windowStart, windowEnd, nil
}

func (s *RightsizingService) QueryMetrics(ctx context.Context, client *govmomi.Client, vms []VMInfo, start, end time.Time) (map[string]VMReport, error) {
	pm := performance.NewManager(client.Client)

	metricIDs, countersByKey, globalWarnings := resolveCounters(ctx, pm)
	if len(metricIDs) == 0 {
		return nil, fmt.Errorf("no desired metrics recognized by this vCenter: %v", globalWarnings)
	}
	if len(globalWarnings) > 0 {
		zap.S().Named("rightsizing_service").Warnw("metric resolution warnings", "warnings", globalWarnings)
	}

	lookback := time.Duration(rightsizingDefaultLookbackHours) * time.Hour
	maxSamples := max(int32(lookback/(time.Duration(rightsizingDefaultIntervalSeconds)*time.Second)), 1)
	results := make(map[string]VMReport, len(vms))

	for i := 0; i < len(vms); i += rightsizingDefaultBatchSize {
		batch := vms[i:min(i+rightsizingDefaultBatchSize, len(vms))]
		specs := make([]types.PerfQuerySpec, len(batch))
		for j, vm := range batch {
			s, e := start, end
			specs[j] = types.PerfQuerySpec{
				Entity:     vm.Ref,
				IntervalId: int32(rightsizingDefaultIntervalSeconds),
				MetricId:   metricIDs,
				StartTime:  &s,
				EndTime:    &e,
				MaxSample:  maxSamples,
			}
		}

		raw, err := pm.Query(ctx, specs)
		if err != nil {
			for _, vm := range batch {
				results[vm.Ref.Value] = VMReport{
					Name:     vm.Name,
					MOID:     vm.Ref.Value,
					Warnings: []string{fmt.Sprintf("batch query failed: %v", err)},
				}
			}
			continue
		}

		for k, v := range parseBatchResults(raw, countersByKey, batch) {
			results[k] = v
		}
	}
	return results, nil
}

func (s *RightsizingService) PersistMetrics(ctx context.Context, vms []VMInfo, vmResults map[string]VMReport, reportID string) error {
	batchSize := rightsizingDefaultBatchSize
	totalBatches := int(math.Ceil(float64(len(vms)) / float64(batchSize)))
	for i := 0; i < len(vms); i += batchSize {
		batchNum := i/batchSize + 1
		batchVMs := vms[i:min(i+batchSize, len(vms))]
		metrics := toStoreMetrics(batchVMs, vmResults)

		if err := s.store.WithTx(ctx, func(txCtx context.Context) error {
			if err := s.store.RightSizing().WriteBatch(txCtx, reportID, metrics); err != nil {
				return err
			}
			return s.store.RightSizing().IncrementWrittenBatchCount(txCtx, reportID)
		}); err != nil {
			return fmt.Errorf("persisting rightsizing batch %d/%d: %w", batchNum, totalBatches, err)
		}
	}
	return nil
}

func (s *RightsizingService) PersistVMWarnings(ctx context.Context, vms []VMInfo, vmResults map[string]VMReport, reportID string) error {
	var noDataWarnings []models.VMWarning
	for _, vm := range vms {
		vmr := vmResults[vm.Ref.Value]
		if len(vmr.Metrics) == 0 {
			warning := "vCenter returned no data for this VM"
			if len(vmr.Warnings) > 0 {
				warning = vmr.Warnings[0]
			}
			noDataWarnings = append(noDataWarnings, models.VMWarning{
				MOID:    vm.Ref.Value,
				VMName:  vm.Name,
				Warning: warning,
			})
		}
	}
	if len(noDataWarnings) > 0 {
		if err := s.store.RightSizing().WriteVMWarnings(ctx, reportID, noDataWarnings); err != nil {
			return fmt.Errorf("persisting rightsizing VM warnings: %w", err)
		}
	}
	return nil
}

func (s *RightsizingService) ComputeUtilization(ctx context.Context, reportID string) error {
	if err := s.store.RightSizing().ComputeAndStoreUtilization(ctx, reportID); err != nil {
		return fmt.Errorf("computing VM utilization: %w", err)
	}
	return nil
}

func resolveCounters(ctx context.Context, pm *performance.Manager) (metricIDs []types.PerfMetricId, countersByKey map[int32]*types.PerfCounterInfo, warnings []string) {
	countersByName, err := pm.CounterInfoByName(ctx)
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("failed to get counter info: %v", err)}
	}
	countersByKey, err = pm.CounterInfoByKey(ctx)
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("failed to get counter info by key: %v", err)}
	}
	for _, name := range desiredMetrics {
		info, ok := countersByName[name]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("metric %q not recognized by this vCenter", name))
			continue
		}
		metricIDs = append(metricIDs, types.PerfMetricId{CounterId: info.Key, Instance: ""})
	}
	return metricIDs, countersByKey, warnings
}

func parseBatchResults(raw []types.BasePerfEntityMetricBase, countersByKey map[int32]*types.PerfCounterInfo, batch []VMInfo) map[string]VMReport {
	results := make(map[string]VMReport, len(batch))

	for _, base := range raw {
		em, ok := base.(*types.PerfEntityMetric)
		if !ok {
			continue
		}
		moid := em.Entity.Value
		metrics := make(map[string]MetricStats)
		var warnings []string
		for _, v := range em.Value {
			series, ok := v.(*types.PerfMetricIntSeries)
			if !ok {
				continue
			}
			info, ok := countersByKey[series.Id.CounterId]
			if !ok {
				continue
			}
			name := info.Name()
			if _, exists := metrics[name]; exists {
				continue
			}
			if len(series.Value) == 0 {
				warnings = append(warnings, fmt.Sprintf("metric %q returned no samples", name))
				continue
			}
			metrics[name] = computeStats(series.Value)
		}
		if len(metrics) == 0 && len(warnings) == 0 {
			warnings = append(warnings, "query succeeded but returned no samples")
		}
		results[moid] = VMReport{MOID: moid, Metrics: metrics, Warnings: warnings}
	}

	for _, vm := range batch {
		if r, exists := results[vm.Ref.Value]; !exists {
			results[vm.Ref.Value] = VMReport{
				Name:     vm.Name,
				MOID:     vm.Ref.Value,
				Warnings: []string{"vCenter returned no data for this VM"},
			}
		} else {
			r.Name = vm.Name
			results[vm.Ref.Value] = r
		}
	}
	return results
}

func computeStats(values []int64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}

	sorted := make([]int64, len(values))
	copy(sorted, values)
	slices.Sort(sorted)

	var sum int64
	for _, v := range values {
		sum += v
	}

	n := len(sorted)
	return MetricStats{
		SampleCount: n,
		Average:     float64(sum) / float64(n),
		P95:         percentile(sorted, 0.95),
		P99:         percentile(sorted, 0.99),
		Max:         float64(sorted[n-1]),
		Latest:      float64(values[len(values)-1]),
	}
}

func percentile(sorted []int64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := max(int(math.Ceil(p*float64(n)))-1, 0)
	if idx >= n {
		idx = n - 1
	}
	return float64(sorted[idx])
}

func toStoreMetrics(batchVMs []VMInfo, vmResults map[string]VMReport) []models.RightSizingMetric {
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
