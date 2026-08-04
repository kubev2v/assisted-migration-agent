package models

import "github.com/vmware/govmomi"

var CollectorRequiredPrivileges = []string{
	"System.Read",
	"System.View",
}

// CollectorStateType represents the current state of the collector.
type CollectorStateType string

func (c CollectorStateType) IsRunning() bool {
	switch c {
	case CollectorStateConnecting:
		fallthrough
	case CollectorStateCollecting:
		fallthrough
	case CollectorStateMetricsCollecting:
		fallthrough
	case CollectorStateParsing:
		return true
	default:
		return false
	}
}

const (
	// CollectorStateReady - credentials saved, waiting for collection request
	CollectorStateReady CollectorStateType = "ready"
	// CollectorStateConnecting - verifying credentials with vCenter
	CollectorStateConnecting CollectorStateType = "connecting"
	// CollectorStateCollecting - async collection in progress
	CollectorStateCollecting CollectorStateType = "collecting"
	// Collectinc metrics
	CollectorStateMetricsCollecting CollectorStateType = "collecting metrics"
	// CollectorStateParsing - parsing collected data into duckdb
	CollectorStateParsing CollectorStateType = "parsing"
	// CollectorStateCollected - collection complete (auto-transitions to ready)
	CollectorStateCollected CollectorStateType = "collected"
	// CollectorStateError - error during connecting or collecting
	CollectorStateError CollectorStateType = "error"

	// Deprecated: only used by v1 collector_work and rightsizing; will be removed with v1.
	CollectorStateRightsizingConnecting CollectorStateType = "rightsizing-connecting"
)

// CollectorStatus holds the current collector state and metadata.
type CollectorStatus struct {
	ID    string
	State CollectorStateType
	Error error
}

// CollectorResult is the shared result struct threaded through collector work units.
// Completed is false by default; the last work unit sets it to true on success.
// Finalize uses it to distinguish cancel (pipeline stopped before last unit) from completion.
type CollectorResult struct {
	Err           error
	Completed     bool
	SQLitePath    string
	Inventory     []byte
	Client        *govmomi.Client
	ChangedGroups []Group
}
