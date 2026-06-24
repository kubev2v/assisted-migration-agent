package models

const InspectionSnapshotName = "assisted-migration-deep-inspector"

// InspectorRequiredPrivileges lists the vSphere privileges needed for deep inspection.
var InspectorRequiredPrivileges = []string{
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
	State   InspectionState
	Details string
	Error   error
}

// InspectionResult is the shared result struct threaded through inspection work units.
// Completed is false by default; the last work unit sets it to true on success.
// Finalize uses it to distinguish cancel (pipeline stopped before last unit) from completion.
type InspectionResult struct {
	Err        error
	Completed  bool
	SnapshotID string
	Concerns   []VmInspectionConcern
}

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
