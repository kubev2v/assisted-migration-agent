package models

import "context"

// InspectorState represents the current state of the Inspector.
type InspectorState string

const (
	// InspectorStateReady - waiting for inspection request
	InspectorStateReady InspectorState = "ready"
	// InspectorStateConnecting - creating vsphere client
	InspectorStateConnecting InspectorState = "connecting"
	// InspectorStateRunning - running inspections on VMs
	InspectorStateRunning InspectorState = "running"
	// InspectorStateCancelled - user stopped inspection
	InspectorStateCancelled InspectorState = "cancelled"
	// InspectorStateDone - Inspection complete
	InspectorStateDone InspectorState = "done"
	// InspectorStateError - error during Inspection
	InspectorStateError InspectorState = "error"
)

// InspectorStatus holds the current Inspector state and metadata.
type InspectorStatus struct {
	State InspectorState
	Error error
}

// InspectionState represents the current state of a VM inspection.
type InspectionState string

const (
	// InspectionStatePending - waiting for inspection
	InspectionStatePending InspectionState = "pending"
	// InspectionStateRunning - the inspection currently running for this vm
	InspectionStateRunning InspectionState = "running"
	// InspectionStateCompleted - inspection finished for this vm
	InspectionStateCompleted InspectionState = "completed"
	// InspectionStateCanceled - Inspection canceled for this vm
	InspectionStateCanceled InspectionState = "canceled"
	// InspectionStateError - error during Inspection
	InspectionStateError InspectionState = "error"
	// InspectionStateNotFound - error during Inspection
	InspectionStateNotFound InspectionState = "not_found"
)

func (i InspectionState) Value() string {
	return string(i)
}

// InspectionStatus holds the current Inspection state for a vm.
type InspectionStatus struct {
	State InspectionState
	Error error
}

const InspectionSnapshotName = "assisted-migration-deep-inspector"

type InspectorWorkBuilder interface {
	Build() InspectorFlow
}

// InspectorWorkUnit represents a unit of work in the collector workflow.
type InspectorWorkUnit struct {
	Status func() InspectorStatus
	Work   func() func(ctx context.Context) (any, error)
}

type InspectorFlow struct {
	Connect InspectorWorkUnit
	Inspect []VmWorkUnit
}

type VmWorkUnit struct {
	VmMoid string
	Work   InspectorWorkUnit
}
