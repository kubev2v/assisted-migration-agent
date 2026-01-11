package vmware

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi"
)

type VMOperator interface {
	CreateSnapshot(context.Context, CreateSnapshotRequest) error
	RemoveSnapshot(context.Context, RemoveSnapshotRequest) error
}

// VMManager provides operations for managing virtual machines within a specific vSphere datacenter.
type VMManager struct {
	gc *govmomi.Client
}

// NewVMManager creates a new VM manager for a specific vSphere datacenter.
//
// Parameters:
//   - gc: an authenticated govmomi client.
//
// Returns an error if:
//   - the datacenter cannot be found using the provided MOID.
func NewVMManager(gc *govmomi.Client) *VMManager {
	return &VMManager{gc: gc}
}

// CreateSnapshot creates a snapshot of a virtual machine, capturing its current state.
//
// Parameters:
//   - ctx: the context for the API request.
//   - req: the CreateSnapshotRequest containing:
//   - VmMoid: the managed object ID of the VM.
//   - SnapshotName: the name for the new snapshot.
//   - Description: a description of the snapshot.
//   - Memory: if true, includes the VM's memory state in the snapshot.
//   - Quiesce: if true, attempts to quiesce the guest file system before taking the snapshot.
//
// Returns an error if:
//   - the snapshot task creation fails,
//   - or the snapshot operation fails during execution.
func (m *VMManager) CreateSnapshot(ctx context.Context, req CreateSnapshotRequest) error {
	vm := m.vmFromMoid(req.VmMoid)

	task, err := vm.CreateSnapshot(ctx, req.SnapshotName, req.Description, req.Memory, req.Quiesce)
	if err != nil {
		return fmt.Errorf("failed to create snapshot task: %w", err)
	}

	err = task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("snapshot creation failed: %w", err)
	}

	return nil
}

// RemoveSnapshot deletes a snapshot and all its children by name from a virtual machine.
//
// Parameters:
//   - ctx: the context for the API request.
//   - req: the RemoveSnapshotRequest containing:
//   - VmMoid: the managed object ID of the VM.
//   - SnapshotName: the name of the snapshot to remove.
//   - Consolidate: if true, consolidates disk files after snapshot removal.
//
// Returns an error if:
//   - the snapshot deletion task cannot be initiated,
//   - or the snapshot deletion fails during execution.
func (m *VMManager) RemoveSnapshot(ctx context.Context, req RemoveSnapshotRequest) error {
	vm := m.vmFromMoid(req.VmMoid)

	task, err := vm.RemoveSnapshot(ctx, req.SnapshotName, true, &req.Consolidate)
	if err != nil {
		return fmt.Errorf("failed to initiate delete snapshot task: %w", err)
	}

	err = task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("snapshot deletion failed: %w", err)
	}

	return nil
}
