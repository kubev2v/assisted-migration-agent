package vmware

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
)

type DatacenterFinder struct {
	finder *find.Finder
	dc     *object.Datacenter
}

func NewDatacenterFinder(ctx context.Context, vc *vim25.Client, datacenterMOID string) (*DatacenterFinder, error) {
	finder := find.NewFinder(vc, true)

	dc, err := finder.Datacenter(ctx, datacenterMOID)
	if err != nil {
		return nil, err
	}

	finder.SetDatacenter(dc)

	return &DatacenterFinder{finder: finder, dc: dc}, nil
}

// FindVMByName searches for a virtual machine by its name within the datacenter.
//
// Parameters:
//   - ctx: the context for the API request.
//   - vmName: the name of the virtual machine to find.
//
// Returns an error if:
//   - the virtual machine with the specified name cannot be found.
//
// Example:
//
//	vm, err := vmMgr.FindVMByName(ctx, "my-vm")
//	if err != nil {
//	    log.Fatalf("Failed to find VM: %v", err)
//	}
func (m *DatacenterFinder) FindVMByName(ctx context.Context, vmName string) (*object.VirtualMachine, error) {
	vm, err := m.finder.VirtualMachine(ctx, vmName)
	if err != nil {
		return nil, fmt.Errorf("failed to find VM with name '%s': %w", vmName, err)
	}

	return vm, nil
}
