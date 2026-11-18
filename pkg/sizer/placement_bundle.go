package sizer

import (
	"slices"
	"uuid"
)

type placementService struct {
	ID uuid.UUID
	Service
}

func newPlacementService(s Service) placementService {
	s.RunsWith = slices.Clone(s.RunsWith)
	s.Avoid = slices.Clone(s.Avoid)

	return placementService{ID: uuid.New(), Service: s}
}

// IsAvoidable reports whether either service avoids the other.
func (s placementService) IsAvoidable(other placementService) bool {
	if slices.IndexFunc(s.Avoid, func(avoidService string) bool {
		return avoidService == other.Name
	}) >= 0 {
		return true
	}

	if slices.IndexFunc(other.Avoid, func(avoidService string) bool {
		return avoidService == s.Name
	}) >= 0 {
		return true
	}

	return false
}

// PlacementPolicy states whether a matching node is allowed or denied.
type PlacementPolicy int

const (
	PlacementDenied PlacementPolicy = iota
	PlacementAllowed
)

// PlacementRule describes node restrictions for one placement.
type PlacementRule struct {
	Policy PlacementPolicy
	Name   string
}

// IsAllowed reports whether this rule allows its matching node.
func (r PlacementRule) IsAllowed() bool {
	return r.Policy == PlacementAllowed
}

// IsDenied reports whether this rule denies its matching node.
func (r PlacementRule) IsDenied() bool {
	return r.Policy == PlacementDenied
}

// IsMatch reports whether the rule targets the given node.
func (r PlacementRule) IsMatch(node Node) bool {
	return r.Name == node.Name
}

// PlacementBundle holds services that must be placed together and their cached requests.
type PlacementBundle struct {
	Id int
	// Count is the number of copies placement must place on distinct nodes.
	Count int
	// AvoidBundleIDs lists the final bundles that must not share a node with this one.
	AvoidBundleIDs     []int
	Rules              []PlacementRule
	AllowedMachineSets []string
	services           []placementService
	totalCPU           float64
	totalMemory        float64
}

// NewPlacementBundle creates an empty bundle with independent placement rules.
func NewPlacementBundle(id int) *PlacementBundle {
	bundle := &PlacementBundle{
		Id:       id,
		services: make([]placementService, 0),
		Rules:    make([]PlacementRule, 0),
	}

	return bundle
}

// ResolveAvoid records the final bundles this bundle must not share a node with.
// Call it once, after joins and Avoid splits produced the final bundle list.
func (b *PlacementBundle) ResolveAvoid(bundles []*PlacementBundle) {
	for _, other := range bundles {
		if other.Id == b.Id || !b.conflictsWith(other) {
			continue
		}
		b.AvoidBundleIDs = append(b.AvoidBundleIDs, other.Id)
	}
}

// conflictsWith reports whether a service of either bundle avoids a service of the other.
func (b *PlacementBundle) conflictsWith(other *PlacementBundle) bool {
	for _, service := range b.services {
		for _, peer := range other.services {
			if service.IsAvoidable(peer) {
				return true
			}
		}
	}
	return false
}

// AddService appends an identified service and updates the cached requests.
func (b *PlacementBundle) AddService(s placementService) {
	s.RunsWith = slices.Clone(s.RunsWith)
	s.Avoid = slices.Clone(s.Avoid)
	b.services = append(b.services, s)
	b.totalCPU += s.RequiredCPU
	b.totalMemory += s.RequiredMemory
}

// RemoveService removes the service with the given ID and updates cached requests.
func (b *PlacementBundle) RemoveService(id uuid.UUID) {
	idx := slices.IndexFunc(b.services, func(s placementService) bool { return s.ID == id })
	if idx >= 0 {
		b.totalCPU -= b.services[idx].RequiredCPU
		b.totalMemory -= b.services[idx].RequiredMemory
		b.services = slices.Delete(b.services, idx, idx+1)
	}
}

// AddPlacementRule adds a node-specific restriction to the bundle.
func (b *PlacementBundle) AddPlacementRule(rule PlacementRule) {
	for i, existing := range b.Rules {
		if existing.Name == rule.Name {
			if !existing.IsDenied() {
				b.Rules[i] = rule
			}
			return
		}
	}
	b.Rules = append(b.Rules, rule)
}

// IsPlacementAllowed checks the node's rule and denies nodes without one.
func (b *PlacementBundle) IsPlacementAllowed(node Node) bool {
	isAllowed := false
	for _, rule := range b.Rules {
		if !rule.IsMatch(node) {
			continue
		}

		if rule.IsDenied() {
			return false
		}

		if rule.IsAllowed() {
			isAllowed = true
		}
	}

	return isAllowed
}

// Services returns the services in the bundle without exposing its cached slice.
func (b *PlacementBundle) Services() []Service {
	services := make([]Service, 0, len(b.services))
	for _, service := range b.services {
		services = append(services, service.Service)
	}

	return services
}

// TotalCPU returns the bundle's cached CPU request.
func (b *PlacementBundle) TotalCPU() float64 {
	return b.totalCPU
}

// TotalMemory returns the bundle's cached memory request in GB.
func (b *PlacementBundle) TotalMemory() float64 {
	return b.totalMemory
}

// MatchesMachineSet reports whether the bundle's allowed machine sets include ms.
// An empty list matches every machine set.
func (b *PlacementBundle) MatchesMachineSet(ms MachineSet) bool {
	return len(b.AllowedMachineSets) == 0 || slices.Contains(b.AllowedMachineSets, ms.Name)
}

// AllowsMachineSet reports whether the bundle may use ms: the set must match the
// bundle's allowed machine sets and, for a control plane, permit workloads.
func (b *PlacementBundle) AllowsMachineSet(ms MachineSet) bool {
	return b.MatchesMachineSet(ms) && allowsWorkloadScheduling(ms)
}

// allowsWorkloadScheduling reports whether ms accepts workloads. A control plane
// does so only when AllowWorkloadScheduling is explicitly true.
func allowsWorkloadScheduling(ms MachineSet) bool {
	return !ms.IsControlPlane || (ms.AllowWorkloadScheduling != nil && *ms.AllowWorkloadScheduling)
}

// HasAvoidConflict reports whether a service already on the node must avoid a bundle service.
func (b *PlacementBundle) HasAvoidConflict(node Node) bool {
	for _, service := range b.services {
		for _, placed := range node.Services {
			if service.IsAvoidable(placementService{Service: placed}) {
				return true
			}
		}
	}
	return false
}
