package vmware

import (
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// vmFromMoid creates a VirtualMachine object reference from a managed object ID.
// This is a helper function that constructs a VM reference without validating
// that the VM actually exists in vSphere.
//
// Parameters:
//   - moid: the managed object ID of the virtual machine.
//
// Returns:
//   - a VirtualMachine object that can be used for subsequent operations.
func (m *VMManager) vmFromMoid(moid string) *object.VirtualMachine {
	ref := types.ManagedObjectReference{
		Type:  "VirtualMachine",
		Value: moid,
	}

	return object.NewVirtualMachine(m.gc.Client, ref)
}
