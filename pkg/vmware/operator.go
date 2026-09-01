package vmware

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/vmware/govmomi"
)

type VMOperator interface {
	CreateSnapshot(context.Context, CreateSnapshotRequest) (string, error)
	RemoveSnapshot(context.Context, RemoveSnapshotRequest) error
	ValidatePrivileges(ctx context.Context, vmId string, requiredPrivileges []string) error
	// Ping performs a cheap liveness probe against vCenter, returning an error
	// when the session/connection is no longer healthy.
	Ping(ctx context.Context) error
}

// VMManager provides operations for managing virtual machines within a specific vSphere datacenter.
type VMManager struct {
	gc       *govmomi.Client
	username string
}

// NewVMManager creates a new VM manager for a specific vSphere datacenter.
//
// Parameters:
//   - gc: an authenticated govmomi client.
//
// Returns an error if:
//   - the datacenter cannot be found using the provided MOID.
func NewVMManager(gc *govmomi.Client, username string) *VMManager {
	return &VMManager{
		gc:       gc,
		username: username,
	}
}

// CreateSnapshot creates a snapshot of a virtual machine, capturing its current state.
//
// Parameters:
//   - ctx: the context for the API request.
//   - req: the CreateSnapshotRequest containing:
//   - VmId: the managed object ID of the VM.
//   - SnapshotName: the name for the new snapshot.
//   - Description: a description of the snapshot.
//   - Memory: if true, includes the VM's memory state in the snapshot.
//   - Quiesce: if true, attempts to quiesce the guest file system before taking the snapshot.
//
// Returns an error if:
//   - the snapshot task creation fails,
//   - or the snapshot operation fails during execution.
func (m *VMManager) CreateSnapshot(ctx context.Context, req CreateSnapshotRequest) (string, error) {
	vm := m.vmFromMoid(req.VmId)

	task, err := vm.CreateSnapshot(ctx, req.SnapshotName, req.Description, req.Memory, req.Quiesce)
	if err != nil {
		return "", fmt.Errorf("failed to create snapshot task: %w", err)
	}

	result, err := task.WaitForResult(ctx)
	if err != nil {
		return "", fmt.Errorf("snapshot creation failed: %w", err)
	}

	snapshotRef, ok := result.Result.(types.ManagedObjectReference)
	if !ok {
		return "", fmt.Errorf("unexpected result type %T", result.Result)
	}

	return snapshotRef.Value, nil
}

// Ping issues a lightweight GetCurrentTime call to verify the vCenter session
// and underlying connection are still alive. Used by long-running inspections
// to guard against dead sessions instead of relying on hard wall-clock timeouts.
func (m *VMManager) Ping(ctx context.Context) error {
	if _, err := methods.GetCurrentTime(ctx, m.gc.RoundTripper); err != nil {
		return fmt.Errorf("vCenter liveness probe failed: %w", err)
	}
	return nil
}

// RemoveSnapshot deletes a snapshot and all its children by name from a virtual machine.
//
// Parameters:
//   - ctx: the context for the API request.
//   - req: the RemoveSnapshotRequest containing:
//   - SnapshotId: the id of the snapshot to remove.
//   - Consolidate: if true, consolidates disk files after snapshot removal.
//
// Returns an error if:
//   - the snapshot deletion task cannot be initiated,
//   - or the snapshot deletion fails during execution.
func (m *VMManager) RemoveSnapshot(ctx context.Context, req RemoveSnapshotRequest) error {
	snapshotRef := types.ManagedObjectReference{
		Type:  "VirtualMachineSnapshot",
		Value: req.SnapshotId,
	}

	r := types.RemoveSnapshot_Task{
		This:           snapshotRef,
		RemoveChildren: true,
		Consolidate:    &req.Consolidate,
	}

	res, err := methods.RemoveSnapshot_Task(ctx, m.gc.RoundTripper, &r)
	if err != nil {
		return fmt.Errorf("failed to start snapshot removal: %w", err)
	}

	// Wait for task
	task := object.NewTask(m.gc.Client, res.Returnval)

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("snapshot removal failed: %w", err)
	}

	return nil
}
