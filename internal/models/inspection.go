package models

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// maxInspectionErrorLen caps the length of the error string persisted/exposed
// via the API. virt-inspector/libguestfs failures dump kilobytes of verbose
// launch trace; the full text still goes to the agent logs at the failure site.
const maxInspectionErrorLen = 600

// condenseInspectionError trims verbose libguestfs/virt-inspector debug output
// down to the salient failure line so the persisted and API-exposed error stays
// readable. Falls back to a length cap when no known marker is present.
func condenseInspectionError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	// The "libguestfs: error:" line names the real cause; keep our own wrapping
	// context (everything *before* the marker) plus that line, dropping the trace.
	if idx := strings.LastIndex(msg, "libguestfs: error:"); idx >= 0 {
		cause := msg[idx:]
		if nl := strings.IndexByte(cause, '\n'); nl >= 0 {
			cause = cause[:nl]
		}
		cause = strings.TrimSpace(cause)

		// Wrapping context is whatever precedes the marker. It is often a
		// single-line wrapped error (no newline), so we must slice at the marker
		// index, not at the first newline — otherwise prefix == whole message and
		// the cause gets duplicated onto itself.
		prefix := strings.TrimRight(strings.TrimSpace(msg[:idx]), ": \t\n")
		if prefix != "" {
			msg = prefix + ": " + cause
		} else {
			msg = cause
		}
	}

	if len(msg) > maxInspectionErrorLen {
		// Truncate on a rune boundary: a raw byte cut can split a multibyte rune,
		// producing invalid UTF-8. DuckDB VARCHAR rejects invalid UTF-8, which
		// would fail the terminal-status write and strand the VM in "running".
		truncated := msg[:maxInspectionErrorLen]
		for len(truncated) > 0 && !utf8.ValidString(truncated) {
			truncated = truncated[:len(truncated)-1]
		}
		msg = truncated + "… (truncated; see agent logs)"
	}
	return errors.New(msg)
}

const (
	// V1 API snapshot name
	InspectionSnapshotName = "assisted-migration-deep-inspector"

	// V2 API snapshot names
	VirtInspectionSnapshotName = "assisted-migration-virt-inspector"
	V2VInspectionSnapshotName  = "assisted-migration-v2v-inspector"
)

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

func TerminalStatus(result InspectionResult) InspectionStatus {
	switch {
	case result.Err != nil && (errors.Is(result.Err, context.Canceled) || errors.Is(result.Err, context.DeadlineExceeded)):
		return InspectionStatus{State: InspectionStateCanceled, Details: "canceled"}
	case result.Err != nil:
		return InspectionStatus{State: InspectionStateError, Error: condenseInspectionError(result.Err)}
	case result.Completed:
		return InspectionStatus{State: InspectionStateCompleted, Details: "completed"}
	default:
		return InspectionStatus{State: InspectionStateCanceled, Details: "canceled"}
	}
}

// InspectionResult is the shared result struct threaded through inspection work units.
// Completed is false by default; the last work unit sets it to true on success.
// Finalize uses it to distinguish cancel (pipeline stopped before last unit) from completion.
type InspectionResult struct {
	Err        error
	Completed  bool
	SnapshotId string
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
