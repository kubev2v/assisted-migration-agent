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
	// InspectionStateNotStarted - Inspection not started for this VM
	InspectionStateNotStarted InspectionState = "not_started"
)

func (i InspectionState) Value() string {
	return string(i)
}

// InspectionStatus holds the current Inspection state for a vm.
type InspectionStatus struct {
	State InspectionState
	Error error
}

// InspectionResult is the shared result struct threaded through inspection work units.
// InspectionResult Todo: pass here data between inspection phase to saving step
type InspectionResult struct{}

// VmInspectionResult is one persisted inspection run for a VM (ordered by inspection_id; CreatedAt is unset).
type VmInspectionResult struct {
	InspectionID int64
	VMID         string
	Concerns     []VmInspectionConcern
}

// VmInspectionConcern is one concern row under a VmInspectionResult.
type VmInspectionConcern struct {
	Category string
	Label    string
	Msg      string
}
