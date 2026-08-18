// Package inventoryutil holds small inventory-derivation helpers shared across
// the agent (the connected console push and the disconnected download bundle).
package inventoryutil

import (
	v1 "github.com/kubev2v/migration-planner/api/v1alpha1"
	agentAPI "github.com/kubev2v/migration-planner/api/v1alpha1/agent"
)

// NewSourceSubsetUpdate builds the agent-facing subset payload from a group
// inventory, deriving the vCenter ID and VM count. Keeping this in one place
// ensures the connected push (pkg/console) and the disconnected download bundle
// produce identical subset files.
func NewSourceSubsetUpdate(name string, inv v1.Inventory) agentAPI.SourceSubsetUpdate {
	var vcenterID *string
	if inv.VcenterId != "" {
		vcenterID = &inv.VcenterId
	}

	vmsCount := 0
	for _, cluster := range inv.Clusters {
		vmsCount += cluster.Vms.Total
	}

	return agentAPI.SourceSubsetUpdate{
		VcenterId: vcenterID,
		Name:      name,
		VmsCount:  &vmsCount,
		Inventory: inv,
	}
}
