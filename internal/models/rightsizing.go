package models

import "time"

// Store I/O types (RightSizingXxx — capital S).
// These match the DuckDB schema in 008_rightsizing.sql and are used exclusively
// by RightSizingStore in internal/store/rightsizing.go.

// RightSizingReport is the input type for RightSizingStore.CreateReport.
type RightSizingReport struct {
	VCenter             string
	ClusterID           string
	IntervalID          int
	WindowStart         time.Time
	WindowEnd           time.Time
	ExpectedSampleCount int
}

// RightSizingMetric holds aggregated stats for one VM × metric key, used by RightSizingStore.WriteBatch.
// Its stat fields (SampleCount, Average, etc.) mirror RightsizingMetricStats intentionally:
// this is a flat row type for bulk DB insertion; RightsizingMetricStats composes into the nested API read model.
type RightSizingMetric struct {
	VMName      string
	MOID        string
	MetricKey   string
	SampleCount int
	Average     float64
	P95         float64
	P99         float64
	Max         float64
	Latest      float64
}

// InventoryVM is a VM read from the local inventory (vinfo table).
type InventoryVM struct {
	ID   string // MoRef value, e.g. "vm-12345"
	Name string
}

// RightsizingCollectionStateType is the state of an async rightsizing collection run.
type RightsizingCollectionStateType string

const (
	RightsizingCollectionStateConnecting  RightsizingCollectionStateType = "connecting"
	RightsizingCollectionStateDiscovering RightsizingCollectionStateType = "discovering"
	RightsizingCollectionStateQuerying    RightsizingCollectionStateType = "querying"
	RightsizingCollectionStatePersisting  RightsizingCollectionStateType = "persisting"
	RightsizingCollectionStateCompleted   RightsizingCollectionStateType = "completed"
	RightsizingCollectionStateError       RightsizingCollectionStateType = "error"
)

// RightsizingCollectionStatus is the status type threaded through the work pipeline.
// During the persisting phase, BatchNum and TotalBatches describe batch progress.
type RightsizingCollectionStatus struct {
	State        RightsizingCollectionStateType
	Error        error
	BatchNum     int // 1-indexed; 0 = not in batch phase
	TotalBatches int
}

// RightsizingCollectionResult is the result type threaded through the work pipeline.
type RightsizingCollectionResult struct {
	ReportID string
}

// API read-model types (RightsizingXxx — lowercase s).
// These are returned by RightsizingService and consumed by the HTTP handler layer.

// RightsizingParams holds request parameters for the TriggerCollection service call.
type RightsizingParams struct {
	Credentials
	NameFilter  string
	ClusterID   string
	LookbackH   int // hours; e.g. 720 = 30 days
	IntervalID  int // vSphere interval in seconds (300=day, 1800=week, 7200=month)
	BatchSize   int
	DiscoverVMs bool // true = query vSphere live; false (default) = use local inventory
}

// RightsizingMetricStats holds per-metric aggregated statistics for the API read model.
type RightsizingMetricStats struct {
	SampleCount int
	Average     float64
	P95         float64
	P99         float64
	Max         float64
	Latest      float64
}

// RightsizingVMReport groups all metric stats for a single VM in the API read model.
type RightsizingVMReport struct {
	Name     string
	MOID     string
	Metrics  map[string]RightsizingMetricStats
	Warnings []string // non-empty when VM was queried but had no metrics data
}

// VMWarning is a VM that was queried but had no historical metrics data.
type VMWarning struct {
	MOID    string
	VMName  string
	Warning string
}

// RightsizingReportSummary is the API read model returned by ListReports (metadata only, no VM metrics).
type RightsizingReportSummary struct {
	ID                  string
	VCenter             string
	ClusterID           string
	WindowStart         time.Time
	WindowEnd           time.Time
	IntervalID          int
	ExpectedSampleCount int
	CreatedAt           time.Time
}

// RightsizingReport is the API read model returned by GetReport (includes full VM metrics).
type RightsizingReport struct {
	ID                  string
	VCenter             string
	ClusterID           string
	WindowStart         time.Time
	WindowEnd           time.Time
	IntervalID          int
	ExpectedSampleCount int
	VMs                 []RightsizingVMReport
	CreatedAt           time.Time
}

// VmUtilizationDetails holds the full utilization breakdown for a single VM.
// Returned by GET /vms/{id}/utilization.
type VmUtilizationDetails struct {
	MOID       string
	VMName     string
	CpuAvg     float64
	CpuP95     float64
	CpuMax     float64
	CpuLatest  float64
	MemAvg     float64
	MemP95     float64
	MemMax     float64
	MemLatest  float64
	Disk       float64
	Confidence float64
}
