package inventoryutil

import (
	"testing"

	v1 "github.com/kubev2v/migration-planner/api/v1alpha1"
)

func TestNewSourceSubsetUpdate(t *testing.T) {
	inv := v1.Inventory{
		VcenterId: "vc-1",
		Clusters: map[string]v1.InventoryData{
			"cluster-a": {Vms: v1.VMs{Total: 3}},
			"cluster-b": {Vms: v1.VMs{Total: 7}},
		},
	}

	subset := NewSourceSubsetUpdate("group-1", inv)

	if subset.Name != "group-1" {
		t.Errorf("Name = %q, want %q", subset.Name, "group-1")
	}
	if subset.VcenterId == nil || *subset.VcenterId != "vc-1" {
		t.Errorf("VcenterId = %v, want vc-1", subset.VcenterId)
	}
	if subset.VmsCount == nil || *subset.VmsCount != 10 {
		t.Errorf("VmsCount = %v, want 10", subset.VmsCount)
	}
	if subset.Inventory.VcenterId != "vc-1" {
		t.Errorf("Inventory.VcenterId = %q, want vc-1", subset.Inventory.VcenterId)
	}
}

func TestNewSourceSubsetUpdate_EmptyVcenter(t *testing.T) {
	subset := NewSourceSubsetUpdate("group-empty", v1.Inventory{})

	if subset.VcenterId != nil {
		t.Errorf("VcenterId = %v, want nil for empty vCenter", subset.VcenterId)
	}
	if subset.VmsCount == nil || *subset.VmsCount != 0 {
		t.Errorf("VmsCount = %v, want 0", subset.VmsCount)
	}
}
