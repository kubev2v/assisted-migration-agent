package sizer

import (
	"fmt"
	"maps"
	"sort"
)

type Nodes []Node

type cluster struct {
	MachineSets map[string]MachineSet
	Nodes       Nodes
}

// newCluster registers machine sets without creating nodes.
func newCluster(machineSets ...MachineSet) *cluster {
	c := cluster{
		MachineSets: make(map[string]MachineSet, len(machineSets)),
	}

	for _, ms := range machineSets {
		c.MachineSets[ms.Name] = ms
	}

	return &c
}

// Place tries to place a bundle on an existing node allowed by its placement rules.
// It checks the bundle's total CPU and memory against each node's remaining capacity.
// It returns the updated node and true on success, or a zero Node and false when no
// existing node accepts the bundle. The caller creates new nodes when needed.
func (c *cluster) Place(pb PlacementBundle) (Node, bool) {
	if len(c.Nodes) == 0 {
		return Node{}, false
	}

	for i, n := range c.Nodes {
		if !pb.IsPlacementAllowed(n) {
			continue
		}

		cpuRequested := n.CPU.Requested + pb.totalCPU
		memRequested := n.Memory.Requested + pb.totalMemory

		if cpuRequested > float64(n.CPU.Total) || memRequested > float64(n.Memory.Total) {
			continue
		}

		n.Services = append(n.Services, pb.Services()...)
		n.CPU.Requested = cpuRequested
		n.Memory.Requested = memRequested

		c.Nodes[i] = n

		return n, true
	}

	return Node{}, false
}

// ApplyEligibilityRules records one explicit rule for every existing node.
// A node is allowed only when its machine set is eligible and it hosts no avoid conflict;
// every other node is denied, so placement never depends on a missing rule.
func (c *cluster) ApplyEligibilityRules(pb *PlacementBundle) {
	for _, node := range c.Nodes {
		policy := PlacementAllowed
		ms, ok := c.MachineSets[node.MachineSetName]
		if !ok || !pb.AllowsMachineSet(ms) || pb.HasAvoidConflict(node) {
			policy = PlacementDenied
		}

		pb.AddPlacementRule(PlacementRule{Policy: policy, Name: node.Name})
	}
}

// CreateAndPlace creates a fitting node for a bundle after Place finds no existing node.
// Among eligible machine sets, it chooses the one with the most usable CPU.
func (c *cluster) CreateAndPlace(pb *PlacementBundle) (Node, error) {
	var chosen MachineSet
	var chosenCPU float64

	// sort machinesets to have a deterministic response
	keys := make([]string, 0, len(c.MachineSets))
	for k := range maps.Keys(c.MachineSets) {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		ms, _ := c.MachineSets[key]
		if !pb.AllowsMachineSet(ms) {
			continue
		}

		availableCPU := float64(ms.CPU) - kubeletCPU(ms.CPU)
		availableMemory := float64(ms.Memory) - kubeletMemory(ms.Memory)
		if ms.IsControlPlane && ms.ControlPlaneReservedResources != nil {
			availableCPU -= ms.ControlPlaneReservedResources.CPU
			availableMemory -= ms.ControlPlaneReservedResources.Memory
		}

		if pb.TotalCPU() > availableCPU || pb.TotalMemory() > availableMemory {
			continue
		}

		if availableCPU > chosenCPU {
			chosen = ms
			chosenCPU = availableCPU
		}
	}

	if chosenCPU == 0 {
		return Node{}, NewNotEnoughResourcesError(pb.TotalCPU(), pb.TotalMemory())
	}

	node := c.CreateNode(chosen)

	pb.AddPlacementRule(PlacementRule{Policy: PlacementAllowed, Name: node.Name})
	placedNode, placed := c.Place(*pb)
	if !placed {
		return Node{}, NewNotEnoughResourcesError(pb.TotalCPU(), pb.TotalMemory())
	}

	return placedNode, nil
}

// CreateNode adds a machine-set node with its baseline reservations already requested.
func (c *cluster) CreateNode(ms MachineSet) Node {
	cpuRequested := kubeletCPU(ms.CPU)
	memoryRequested := kubeletMemory(ms.Memory)

	if ms.IsControlPlane && ms.ControlPlaneReservedResources != nil {
		cpuRequested += ms.ControlPlaneReservedResources.CPU
		memoryRequested += ms.ControlPlaneReservedResources.Memory
	}

	node := Node{
		Name:           fmt.Sprintf("Node %d", len(c.Nodes)+1),
		MachineSetName: ms.Name,
		IsControlPlane: ms.IsControlPlane,
		CPU:            Resource{Requested: cpuRequested, Total: ms.CPU},
		Memory:         Resource{Requested: memoryRequested, Total: ms.Memory},
		Services:       []Service{},
	}

	c.Nodes = append(c.Nodes, node)

	return node
}

// getNode returns the stored node for a name.
func (c *cluster) getNode(name string) *Node {
	for i := range c.Nodes {
		if c.Nodes[i].Name == name {
			return &c.Nodes[i]
		}
	}
	return nil
}

// kubeletCPU preserves the CPU reservations used by the original calculator.
func kubeletCPU(cpu int) float64 {
	reserved := 0.06
	if cpu > 1 {
		reserved += 0.01 * float64(min(cpu-1, 1))
	}
	if cpu > 2 {
		reserved += 0.005 * float64(min(cpu-2, 2))
	}
	if cpu > 4 {
		reserved += 0.0025 * float64(cpu-4)
	}
	return reserved
}

// kubeletMemory preserves the original memory tiers, including the boundary at 4 GB.
func kubeletMemory(memory int) float64 {
	if memory < 1 {
		return 0.255
	}
	reserved := 0.0
	if memory > 1 {
		if memory <= 4 {
			reserved += 0.25 * float64(memory-1)
		} else {
			reserved += 1
		}
	}
	if memory > 4 {
		reserved += 0.2 * float64(min(memory-4, 4))
	}
	if memory > 8 {
		reserved += 0.1 * float64(min(memory-8, 8))
	}
	if memory > 16 {
		reserved += 0.06 * float64(min(memory-16, 112))
	}
	if memory > 128 {
		reserved += 0.02 * float64(memory-128)
	}
	return reserved
}
