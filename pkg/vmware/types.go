package vmware

// RemoveSnapshotRequest contains the parameters needed to remove a snapshot from a VM.
//
// Fields:
//   - VmMoid: the managed object ID of the virtual machine.
//   - SnapshotName: the name of the snapshot to remove.
//   - Consolidate: if true, consolidates disk files after snapshot removal.
type RemoveSnapshotRequest struct {
	VmMoid       string
	SnapshotName string
	Consolidate  bool
}

// CreateSnapshotRequest contains the parameters needed to create a snapshot of a VM.
//
// Fields:
//   - VmMoid: the managed object ID of the virtual machine.
//   - SnapshotName: the name to assign to the new snapshot.
//   - Description: a description of the snapshot's purpose or content.
//   - Memory: if true, includes the VM's memory state in the snapshot (for running VMs).
//   - Quiesce: if true, attempts to quiesce the guest file system before snapshotting.
type CreateSnapshotRequest struct {
	VmMoid       string
	SnapshotName string
	Description  string
	Memory       bool
	Quiesce      bool
}
