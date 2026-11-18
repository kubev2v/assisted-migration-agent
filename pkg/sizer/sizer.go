package sizer

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

const (
	avoidLink int = iota - 1
	noLink
	runWithLink
)

type Sizer struct{}

// NewSizer creates a stateless calculator that can serve independent concurrent requests.
func NewSizer() *Sizer {
	return &Sizer{}
}

// Calculate places workload copies on eligible nodes and reports their resources.
func (s *Sizer) Calculate(ctx context.Context, machineSets []MachineSet, workloads []Workload, detailed bool) (*SizerResponse, error) {
	if err := validate(machineSets, workloads); err != nil {
		return nil, err
	}

	// Preserve request order within worker and control-plane templates without modifying input.
	orderedMachineSets := make([]MachineSet, 0, len(machineSets))
	for _, ms := range machineSets {
		if !ms.IsControlPlane {
			orderedMachineSets = append(orderedMachineSets, ms)
		}
	}
	for _, ms := range machineSets {
		if ms.IsControlPlane {
			orderedMachineSets = append(orderedMachineSets, ms)
		}
	}

	c := newCluster(orderedMachineSets...)

	activeWorkloads := make([]Workload, 0, len(workloads))
	for _, w := range workloads {
		if w.Count <= 0 {
			continue
		}
		activeWorkloads = append(activeWorkloads, w)
	}

	bundles := s.createPlacementBundles(activeWorkloads, orderedMachineSets)

	for _, bundle := range bundles {
		bundle.ResolveAvoid(bundles)
	}

	bundlesByID := make(map[int]*PlacementBundle, len(bundles))
	for _, bundle := range bundles {
		bundlesByID[bundle.Id] = bundle
	}

	// Place every copy of a bundle; each placed copy is denied on its node so copies never share one.
	for _, bundle := range bundles {
		c.ApplyEligibilityRules(bundle)

		for bundle.Count > 0 {
			var err error
			node, placed := c.Place(*bundle)
			if !placed {
				node, err = c.CreateAndPlace(bundle)
				if err != nil {
					return nil, err
				}
			}

			bundle.AddPlacementRule(PlacementRule{Policy: PlacementDenied, Name: node.Name})
			for _, id := range bundle.AvoidBundleIDs {
				peer, ok := bundlesByID[id]
				if !ok {
					continue
				}
				peer.AddPlacementRule(PlacementRule{Policy: PlacementDenied, Name: node.Name})
			}
			bundle.Count--
		}
	}

	return s.response(c, detailed)
}

// response reports placed services separately from scheduling reservations.
func (s *Sizer) response(c *cluster, detailed bool) (*SizerResponse, error) {
	result := &SizerResponse{NodeCount: len(c.Nodes)}
	if detailed {
		result.Nodes = make([]Node, 0, len(c.Nodes))
	}

	var limits ResourceLimits
	var availableCPU, availableMemory float64
	hasLimits := false

	for _, node := range c.Nodes {
		result.TotalCPU += node.CPU.Total
		result.TotalMemory += node.Memory.Total

		cpuAvailable := float64(node.CPU.Total) - kubeletCPU(node.CPU.Total)
		memoryAvailable := float64(node.Memory.Total) - kubeletMemory(node.Memory.Total)
		availableCPU += cpuAvailable
		availableMemory += memoryAvailable

		output := node
		output.CPU = Resource{Total: node.CPU.Total}
		output.Memory = Resource{Total: node.Memory.Total}

		var nodeLimits ResourceLimits
		nodeHasLimits := false

		for _, service := range node.Services {
			result.ResourceConsumption.CPU += service.RequiredCPU
			result.ResourceConsumption.Memory += service.RequiredMemory
			output.CPU.Requested += service.RequiredCPU
			output.Memory.Requested += service.RequiredMemory

			cpu, memory := service.LimitCPU, service.LimitMemory
			if cpu == 0 {
				cpu = service.RequiredCPU
			}
			if memory == 0 {
				memory = service.RequiredMemory
			}

			limits.CPU += cpu
			limits.Memory += memory
			nodeLimits.CPU += cpu
			nodeLimits.Memory += memory
			nodeHasLimits = nodeHasLimits || service.LimitCPU != 0 || service.LimitMemory != 0
		}

		hasLimits = hasLimits || nodeHasLimits

		if detailed {
			output.Services = slices.Clone(node.Services)

			if nodeHasLimits {
				output.CPU.Limits = nodeLimits.CPU
				output.Memory.Limits = nodeLimits.Memory

				if cpuAvailable > 0 {
					output.CPU.OverCommitRatio = nodeLimits.CPU / cpuAvailable
				}
				if memoryAvailable > 0 {
					output.Memory.OverCommitRatio = nodeLimits.Memory / memoryAvailable
				}
			}

			result.Nodes = append(result.Nodes, output)
		}
	}

	if hasLimits {
		result.ResourceConsumption.Limits = &limits
		result.ResourceConsumption.OverCommitRatio = &OverCommitRatio{}

		if availableCPU > 0 {
			result.ResourceConsumption.OverCommitRatio.CPU = limits.CPU / availableCPU
		}
		if availableMemory > 0 {
			result.ResourceConsumption.OverCommitRatio.Memory = limits.Memory / availableMemory
		}
	}

	return result, nil
}

func (s *Sizer) createPlacementBundles(workloads []Workload, ms []MachineSet) []*PlacementBundle {
	machineSets := make(map[string]MachineSet)
	for _, m := range ms {
		machineSets[m.Name] = m
	}

	bundles := make(map[int]*PlacementBundle)
	serviceBundle := make([]int, 0)   // bundle ID holding each service
	serviceWorkload := make([]int, 0) // workload each service came from

	// A service's position in the flattened order is its ID and matrix index.
	i := 0
	services := make([]placementService, 0)
	for workloadIndex, w := range workloads {
		for _, s := range w.Services {
			service := newPlacementService(s)
			services = append(services, service)
			serviceWorkload = append(serviceWorkload, workloadIndex)
			pb := NewPlacementBundle(i)
			pb.Count = w.Count
			pb.AllowedMachineSets = slices.Clone(w.AllowedMachineSets)
			pb.AddService(service)

			bundles[i] = pb
			serviceBundle = append(serviceBundle, i)
			i++
		}

	}

	total := len(services)
	matrix := make([][]int, total)

	for i := range total {
		matrix[i] = make([]int, total)
		for ii := range total {
			matrix[i][ii] = noLink
		}
	}

	// Rows and columns follow the flattened service order; the diagonal stays
	// noLink because a service never links to itself.
	// Other cells hold runWithLink, avoidLink, or noLink, and links are
	// stored in both directions.
	pos := 0
	for _, w := range workloads {
		for _, s := range w.Services {
			// Link RunWith peers.
			for _, name := range s.RunsWith {
				rPos := slices.IndexFunc(services, func(other placementService) bool { return other.Name == name })
				if rPos >= 0 && rPos != pos && matrix[pos][rPos] != avoidLink {
					matrix[pos][rPos] = runWithLink
					matrix[rPos][pos] = runWithLink
				}
			}

			// Link Avoid peers; Avoid overwrites RunWith in the matrix.
			for _, name := range s.Avoid {
				aPos := slices.IndexFunc(services, func(other placementService) bool { return other.Name == name })
				if aPos >= 0 && aPos != pos {
					matrix[pos][aPos] = avoidLink
					matrix[aPos][pos] = avoidLink
				}
			}

			pos++
		}
	}

	// Turn the matrix into placement bundles.
	// Rules: create one bundle per service (services of a workload are not
	// colocated by default), then merge RunWith peers, then split Avoid peers.
	// Avoid wins over RunWith.
	//
	// RunWith merges bundles {A,B} and {C,D} into one bundle {A,B,C,D} when any
	// service of one runs with a service of the other; the destination policy
	// below picks the bundle whose identity and count survive, while the allowed
	// machine sets are merged (an empty list means "any" and absorbs the other).
	//
	// Avoid then moves the later service of a conflicting pair out of a shared
	// bundle. The split service keeps its original workload Count and
	// AllowedMachineSets, which today are looked up through the service name, so
	// duplicate service names inside one bundle are ambiguous.
	//
	// Every bundle carries its workload copies; placement expands them into
	// distinct nodes.

	// maxAllowedSetCPU returns the largest CPU capacity among the allowed machine
	// sets of a bundle, used only to break ties between equally constrained bundles.
	maxAllowedSetCPU := func(b *PlacementBundle, machineSets map[string]MachineSet) int {
		maxCpu := 0
		for _, ams := range b.AllowedMachineSets {
			if ms, ok := machineSets[ams]; ok {
				maxCpu = max(ms.CPU, maxCpu)
			}
		}
		return maxCpu
	}

	// Resolve RunWith between services that sit in different bundles.
	for i := range total {
		currentBundle := bundles[serviceBundle[i]]
		for j := i + 1; j < total; j++ {
			if matrix[j][i] == runWithLink {
				peerBundleID := serviceBundle[j]

				if peerBundleID == currentBundle.Id {
					continue
				}

				peerBundle := bundles[peerBundleID]

				destination := currentBundle
				source := peerBundle
				// Pick the surviving bundle in this order:
				// 1. an unconstrained bundle (empty AllowedMachineSets) wins;
				// 2. the bundle with more machine sets wins;
				// 3. the bundle with the larger CPU capacity wins;
				// 4. on a full tie, the current bundle wins.
				switch len(peerBundle.AllowedMachineSets) {
				case 0:
					if len(currentBundle.AllowedMachineSets) == 0 {
						break
					}
					destination = peerBundle
					source = currentBundle
				default:
					if len(currentBundle.AllowedMachineSets) == 0 {
						break
					}

					if len(peerBundle.AllowedMachineSets) > len(currentBundle.AllowedMachineSets) {
						destination = peerBundle
						source = currentBundle
					}

					if len(peerBundle.AllowedMachineSets) == len(currentBundle.AllowedMachineSets) {
						if maxAllowedSetCPU(peerBundle, machineSets) > maxAllowedSetCPU(currentBundle, machineSets) {
							destination = peerBundle
							source = currentBundle
						}
					}
				}

				// The joined bundle may use any machine set either member may use.
				// An empty list means "any" and absorbs the other list.
				if len(destination.AllowedMachineSets) == 0 || len(source.AllowedMachineSets) == 0 {
					destination.AllowedMachineSets = nil
				} else {
					for _, name := range source.AllowedMachineSets {
						if !slices.Contains(destination.AllowedMachineSets, name) {
							destination.AllowedMachineSets = append(destination.AllowedMachineSets, name)
						}
					}
				}

				// Move every service of the source bundle, since they must run together.
				for id := range serviceBundle {
					if serviceBundle[id] == source.Id {
						destination.AddService(services[id])
						source.RemoveService(services[id].ID)

						serviceBundle[id] = destination.Id
					}
				}

				// Joined services must all be placed, so the merged bundle carries the larger count.
				destination.Count = max(destination.Count, source.Count)

				currentBundle = destination
			}
		}
	}

	// Resolve Avoid after RunWith by splitting the later service of a conflicting
	// pair out of its shared bundle. RemoveService matches services by ID, so
	// duplicate names cannot move the wrong entry.
	nextBundleID := len(bundles)
	for i := range total {
		for j := i + 1; j < total; j++ {
			if matrix[i][j] == avoidLink && serviceBundle[i] == serviceBundle[j] {
				pb := NewPlacementBundle(nextBundleID)
				source := bundles[serviceBundle[j]]
				origin := workloads[serviceWorkload[j]]
				pb.Count = origin.Count
				pb.AllowedMachineSets = slices.Clone(origin.AllowedMachineSets)

				source.RemoveService(services[j].ID)
				pb.AddService(services[j])

				bundles[nextBundleID] = pb
				serviceBundle[j] = nextBundleID
				nextBundleID++
			}
		}
	}

	// Bundle IDs are allocated sequentially and never reused, so walking them in
	// order yields deterministic placement.
	result := make([]*PlacementBundle, 0, len(bundles))
	for i := range len(bundles) {
		b := bundles[i]
		if len(b.services) == 0 {
			continue
		}
		result = append(result, b)
	}

	return result
}

// validate rejects incomplete or contradictory requests before placement.
func validate(machineSets []MachineSet, workloads []Workload) error {
	if len(machineSets) == 0 {
		return errors.New("machine sets are required")
	}

	if len(workloads) == 0 {
		return errors.New("workloads are required")
	}

	// Service names identify RunsWith and Avoid references across all workloads.
	seenServices := make(map[string]struct{})
	for _, workload := range workloads {
		for _, service := range workload.Services {
			if _, ok := seenServices[service.Name]; ok {
				return fmt.Errorf("duplicate service name %q", service.Name)
			}
			seenServices[service.Name] = struct{}{}
		}
	}

	// Machine set names identify templates in both affinity rules and the cluster.
	machineSetNames := make(map[string]struct{}, len(machineSets))
	for _, ms := range machineSets {
		if _, ok := machineSetNames[ms.Name]; ok {
			return fmt.Errorf("duplicate machine set name %q", ms.Name)
		}
		machineSetNames[ms.Name] = struct{}{}
	}

	for _, workload := range workloads {
		for _, name := range workload.AllowedMachineSets {
			if _, ok := machineSetNames[name]; !ok {
				return fmt.Errorf("allowed machine set %q does not exist", name)
			}
		}

		permitted := false
		for _, ms := range machineSets {
			if !allowsWorkloadScheduling(ms) {
				continue
			}
			if len(workload.AllowedMachineSets) == 0 || slices.Contains(workload.AllowedMachineSets, ms.Name) {
				permitted = true
				break
			}
		}
		if permitted {
			continue
		}

		if len(workload.AllowedMachineSets) == 0 {
			return errors.New("no machine set allows workload scheduling")
		}
		return fmt.Errorf("machine sets %v do not allow workload scheduling", workload.AllowedMachineSets)
	}

	return nil
}
