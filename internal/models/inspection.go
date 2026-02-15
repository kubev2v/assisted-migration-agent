package models

const InspectionSnapshotName = "assisted-migration-deep-inspector"

// RequiredPrivileges Todo:
// This list should represent the lease permissions required for the inspection.
// The goal is to pass this array to the ValidateUserPrivilegesOnEntity function
// in order to determine whether the user has permission on the VM object.
var RequiredPrivileges = []string{
	"VirtualMachine.State.CreateSnapshot",
	"VirtualMachine.State.RemoveSnapshot",
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

// VMWorkflow represents a collection of steps needed by one vm during the workflow.
type VMWorkflow struct {
	Validate       InspectorWorkUnit
	CreateSnapshot InspectorWorkUnit
	Inspect        InspectorWorkUnit
	Save           InspectorWorkUnit
	RemoveSnapshot InspectorWorkUnit
}

type InspectionWorkBuilder interface {
	Build(string) VMWorkflow
}
