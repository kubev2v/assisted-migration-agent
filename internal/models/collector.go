package models

import "github.com/vmware/govmomi"

var CollectorRequiredPrivileges = []string{
	"System.Read",
	"System.View",
}

// CollectorStateType represents the current state of the collector.
type CollectorStateType string

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

	// Rightsizing phases — reported while the post-collection metrics run is active.
	// All map to CollectorLegacyStateCollecting so the SaaS side sees a single in-progress state.
	CollectorStateRightsizingConnecting  CollectorStateType = "rightsizing-connecting"
	CollectorStateRightsizingDiscovering CollectorStateType = "rightsizing-discovering"
	CollectorStateRightsizingQuerying    CollectorStateType = "rightsizing-querying"
	CollectorStateRightsizingPersisting  CollectorStateType = "rightsizing-persisting"

	// V1 agent status
	CollectorLegacyStateWaitingForCredentials CollectorStateType = "waiting-for-credentials"
	CollectorLegacyStateCollecting            CollectorStateType = "gathering-initial-inventory"
	CollectorLegacyStateError                 CollectorStateType = "error"
	CollectorLegacyStateCollected             CollectorStateType = "up-to-date"
)

// TODO: Need to agree on how to solve this mess
// We need to revisit the saas states like "waiting-for-credentials" which is not pertinent anymore
func (c CollectorStateType) ToV1() CollectorStateType {
	switch c {
	case CollectorStateReady:
		return CollectorLegacyStateWaitingForCredentials
	case CollectorStateConnecting, CollectorStateCollecting, CollectorStateParsing:
		return CollectorLegacyStateCollecting
	case CollectorStateCollected:
		return CollectorLegacyStateCollected
	case CollectorLegacyStateError:
		return CollectorLegacyStateError
	case CollectorStateMetricsCollecting:
		return CollectorLegacyStateCollecting
	case CollectorStateRightsizingConnecting,
		CollectorStateRightsizingDiscovering,
		CollectorStateRightsizingQuerying,
		CollectorStateRightsizingPersisting:
		return CollectorLegacyStateCollecting
	default:
		return "unknown state"
	}
}

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
	Err        error
	Completed  bool
	SQLitePath string
	Inventory  []byte
	Client     *govmomi.Client
}
