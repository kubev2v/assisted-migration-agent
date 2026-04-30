package models

import "time"

// ForecasterRequiredPrivileges lists the vSphere privileges needed by the forecaster
// to create benchmark VMs, copy disks, and manage datastore files.
var ForecasterRequiredPrivileges = []string{
	"Datastore.AllocateSpace",
	"Datastore.Browse",
	"Datastore.DeleteFile",
	"Datastore.FileManagement",
	"VirtualMachine.Inventory.Create",
	"VirtualMachine.Inventory.Delete",
	"VirtualMachine.Provisioning.Clone",
	"VirtualMachine.Interact.PowerOn",
	"VirtualMachine.Config.AddRemoveDevice",
}

// ForecasterState represents the service-level state of the forecaster.
type ForecasterState string

const (
	ForecasterStateReady   ForecasterState = "ready"
	ForecasterStateRunning ForecasterState = "running"
)

// ForecasterStatus holds the current state of the forecaster service.
type ForecasterStatus struct {
	State ForecasterState
	Pairs []ForecastPairStatus
}

// ForecastPairState represents the state of a single datastore pair benchmark.
type ForecastPairState string

const (
	ForecastPairStatePending   ForecastPairState = "pending"
	ForecastPairStatePreparing ForecastPairState = "preparing"
	ForecastPairStateRunning   ForecastPairState = "running"
	ForecastPairStateCompleted ForecastPairState = "completed"
	ForecastPairStateCanceled  ForecastPairState = "canceled"
	ForecastPairStateError     ForecastPairState = "error"
)

// ForecastPairStatus tracks per-pair progress during a forecast run.
type ForecastPairStatus struct {
	State             ForecastPairState
	Error             error
	PairName          string
	SourceDatastore   string
	TargetDatastore   string
	Host              string // ESXi host used (user-specified or auto-selected)
	CompletedRuns     int
	TotalRuns         int
	PrepBytesTotal    int64 // total bytes to upload during prep (fill random)
	PrepBytesUploaded int64 // bytes uploaded so far
}

// ForecastRequest defines the input for starting a forecast.
type ForecastRequest struct {
	Credentials Credentials
	Pairs       []DatastorePair
	DiskSizeGB  int // default: 10
	Iterations  int // default: 5
	Concurrency int // max parallel pairs, default: 1 (sequential)
}

// DatastorePair identifies a source and target datastore for benchmarking.
type DatastorePair struct {
	Name            string
	SourceDatastore string
	TargetDatastore string
	Host            string // optional: pin benchmark to a specific ESXi host
}

// ForecastResult is the result type threaded through forecast pipeline work units.
type ForecastResult struct {
	Runs []BenchmarkRun
}

// BenchmarkRun records one benchmark iteration between two datastores.
type BenchmarkRun struct {
	ID              int64
	SessionID       int64
	PairName        string
	SourceDS        string
	TargetDS        string
	Iteration       int
	DiskSizeGB      int
	PrepDurationSec float64 // time spent on disk creation + random fill (iteration 1 only)
	DurationSec     float64
	ThroughputMBps  float64
	Method          string
	Error           string
	CreatedAt       time.Time
}

// ForecastStats holds computed statistics for a datastore pair's benchmark results.
type ForecastStats struct {
	PairName    string
	SampleCount int
	MeanMBps    float64
	MedianMBps  float64
	MinMBps     float64
	MaxMBps     float64
	StdDevMBps  float64
	CI95Lower   float64
	CI95Upper   float64
	EstPer1TB   EstimateRange
}

// EstimateRange provides best/expected/worst case time estimates for migrating 1TB.
type EstimateRange struct {
	BestCase  time.Duration
	Expected  time.Duration
	WorstCase time.Duration
}

// DatastoreDetail holds datastore info with storage array identification for pair selection.
type DatastoreDetail struct {
	Name           string
	Type           string // "VMFS", "NFS", "VVol", "OTHER"
	CapacityGB     float64
	FreeGB         float64
	StorageVendor  string
	StorageModel   string
	StorageArrayID string // derived from NAA prefix; same value = same physical array
	NAADevices     []string
	Capabilities   []string // intrinsic offload capabilities: "copy-offload", "xcopy", "rdm", "vvol"
}

// PairCapabilityRequest is the input for computing pair-level capabilities.
type PairCapabilityRequest struct {
	Pairs []DatastorePair
}

// PairCapability holds the computed offload capabilities for a source→target pair.
type PairCapability struct {
	PairName        string
	SourceDatastore string
	TargetDatastore string
	Capabilities    []string // "copy-offload", "xcopy", "rdm", "vvol"
}
