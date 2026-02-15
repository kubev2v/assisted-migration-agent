package models

import (
	"context"
)

// InspectorState represents the current state of the Inspector.
type InspectorState string

const (
	// InspectorStateReady - waiting for inspection request
	InspectorStateReady InspectorState = "ready"
	// InspectorStateInitiating - creating vsphere client
	InspectorStateInitiating InspectorState = "Initiating"
	// InspectorStateRunning - running inspections on VMs
	InspectorStateRunning InspectorState = "running"
	// InspectorStateCanceling - inspector cancelling his work
	InspectorStateCanceling InspectorState = "canceling"
	// InspectorStateCanceled - inspection canceled
	InspectorStateCanceled InspectorState = "canceled"
	// InspectorStateCompleted - Inspection complete
	InspectorStateCompleted InspectorState = "completed"
	// InspectorStateError - error during Inspection
	InspectorStateError InspectorState = "error"
)

// InspectorStatus holds the current Inspector state and metadata.
type InspectorStatus struct {
	State InspectorState
	Error error
}

// InspectorWorkUnit represents a unit of work in the collector workflow.
type InspectorWorkUnit struct {
	Work func() func(ctx context.Context) (any, error)
}
