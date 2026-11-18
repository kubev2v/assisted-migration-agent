package sizer

import (
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// bundleExpectation describes one distinct bundle and the copies it carries.
type bundleExpectation struct {
	services []string
	allowed  []string
	cpu      float64
	memory   float64
	copies   int
}

// bundleSnapshot is an order-independent view of a placement bundle.
type bundleSnapshot struct {
	services []string
	allowed  []string
	cpu      float64
	memory   float64
	copies   int
}

// snapshotBundles records bundle contents without relying on service or allowed-list order.
func snapshotBundles(bundles []*PlacementBundle) []bundleSnapshot {
	snapshots := make([]bundleSnapshot, 0, len(bundles))
	for _, bundle := range bundles {
		services := make([]string, 0, len(bundle.Services()))
		for _, service := range bundle.Services() {
			services = append(services, service.Name)
		}
		slices.Sort(services)

		allowed := slices.Clone(bundle.AllowedMachineSets)
		slices.Sort(allowed)

		snapshots = append(snapshots, bundleSnapshot{
			services: services,
			allowed:  allowed,
			cpu:      bundle.TotalCPU(),
			memory:   bundle.TotalMemory(),
			copies:   bundle.Count,
		})
	}
	return snapshots
}

// expectedSnapshots turns distinct bundle expectations into their order-independent snapshots.
func expectedSnapshots(expected []bundleExpectation) []bundleSnapshot {
	snapshots := make([]bundleSnapshot, 0, len(expected))
	for _, bundle := range expected {
		services := slices.Clone(bundle.services)
		slices.Sort(services)

		allowed := slices.Clone(bundle.allowed)
		slices.Sort(allowed)

		snapshots = append(snapshots, bundleSnapshot{
			services: services,
			allowed:  allowed,
			cpu:      bundle.cpu,
			memory:   bundle.memory,
			copies:   bundle.copies,
		})
	}
	return snapshots
}

// cloneWorkloads deeply copies workload slices so specs can detect caller-input mutation.
func cloneWorkloads(workloads []Workload) []Workload {
	if workloads == nil {
		return nil
	}

	cloned := make([]Workload, len(workloads))
	for i, workload := range workloads {
		cloned[i] = workload
		cloned[i].AllowedMachineSets = slices.Clone(workload.AllowedMachineSets)
		cloned[i].Services = slices.Clone(workload.Services)
		for j := range cloned[i].Services {
			cloned[i].Services[j].RunsWith = slices.Clone(workload.Services[j].RunsWith)
			cloned[i].Services[j].Avoid = slices.Clone(workload.Services[j].Avoid)
		}
	}
	return cloned
}

var _ = Describe("createPlacementBundles", func() {
	When("services are grouped into bundles", func() {
		DescribeTable("splits services into independent bundles",
			func(workloads []Workload, machineSets []MachineSet, expected []bundleExpectation) {
				// Arrange
				original := cloneWorkloads(workloads)

				// Act
				bundles := NewSizer().createPlacementBundles(workloads, machineSets)

				// Assert
				Expect(workloads).To(Equal(original))
				Expect(snapshotBundles(bundles)).To(Equal(expectedSnapshots(expected)))
			},
			Entry("keeps each service in its own bundle with the workload's copies and allowed sets",
				[]Workload{
					{Name: "apps", Count: 2, AllowedMachineSets: []string{"worker"}, Services: []Service{
						{Name: "api", RequiredCPU: 1, RequiredMemory: 2},
						{Name: "db", RequiredCPU: 2, RequiredMemory: 4},
					}},
					{Name: "cache", Count: 3, AllowedMachineSets: []string{"memory"}, Services: []Service{
						{Name: "redis", RequiredCPU: .5, RequiredMemory: 1.5},
					}},
				},
				[]MachineSet{{Name: "worker", CPU: 8}, {Name: "memory", CPU: 16}},
				[]bundleExpectation{
					{services: []string{"api"}, allowed: []string{"worker"}, cpu: 1, memory: 2, copies: 2},
					{services: []string{"db"}, allowed: []string{"worker"}, cpu: 2, memory: 4, copies: 2},
					{services: []string{"redis"}, allowed: []string{"memory"}, cpu: .5, memory: 1.5, copies: 3},
				},
			),
			Entry("returns no bundles for an empty workload list",
				[]Workload(nil),
				[]MachineSet(nil),
				[]bundleExpectation(nil),
			),
			Entry("returns no bundles for workloads without services",
				[]Workload{{Name: "empty", Count: 4}},
				[]MachineSet{{Name: "worker", CPU: 8}},
				[]bundleExpectation(nil),
			),
			Entry("keeps a zero-count workload's bundle with zero copies when called directly",
				[]Workload{{Name: "disabled", Count: 0, Services: []Service{{Name: "ignored", RequiredCPU: 1}}}},
				[]MachineSet{{Name: "worker", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"ignored"}, cpu: 1, copies: 0},
				},
			),
			Entry("creates one bundle per service instead of one bundle per copy",
				[]Workload{{Name: "apps", Count: 4, Services: []Service{
					{Name: "api", RequiredCPU: 1, RequiredMemory: 2},
					{Name: "db", RequiredCPU: 2, RequiredMemory: 4},
				}}},
				[]MachineSet{{Name: "worker", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"api"}, cpu: 1, memory: 2, copies: 4},
					{services: []string{"db"}, cpu: 2, memory: 4, copies: 4},
				},
			),
		)
	})

	When("services declare RunsWith links", func() {
		DescribeTable("joins linked services into one bundle",
			func(workloads []Workload, machineSets []MachineSet, expected []bundleExpectation) {
				// Arrange
				original := cloneWorkloads(workloads)

				// Act
				bundles := NewSizer().createPlacementBundles(workloads, machineSets)

				// Assert
				Expect(workloads).To(Equal(original))
				Expect(snapshotBundles(bundles)).To(Equal(expectedSnapshots(expected)))
			},
			Entry("joins RunsWith services across workloads and keeps the larger count",
				[]Workload{
					{Name: "web", Count: 2, Services: []Service{{Name: "api", RequiredCPU: 1, RequiredMemory: 2, RunsWith: []string{"db"}}}},
					{Name: "data", Count: 3, Services: []Service{{Name: "db", RequiredCPU: 2, RequiredMemory: 4}}},
				},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"api", "db"}, cpu: 3, memory: 6, copies: 3},
				},
			),
			Entry("joins a transitive RunsWith chain into one bundle",
				[]Workload{
					{Name: "one", Count: 1, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 2, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2, RunsWith: []string{"c"}}}},
					{Name: "three", Count: 3, Services: []Service{{Name: "c", RequiredCPU: 3, RequiredMemory: 3}}},
				},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a", "b", "c"}, cpu: 6, memory: 6, copies: 3},
				},
			),
			Entry("merges a RunsWith cycle with duplicate and self references only once",
				[]Workload{
					{Name: "one", Count: 2, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 2, RunsWith: []string{"a", "b", "b"}}}},
					{Name: "two", Count: 1, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 4, RunsWith: []string{"a", "a", "b"}}}},
				},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a", "b"}, cpu: 3, memory: 6, copies: 2},
				},
			),
			Entry("ignores self references and unknown service names",
				[]Workload{{Name: "one", Count: 1, Services: []Service{
					{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"a", "missing"}, Avoid: []string{"a", "missing"}},
					{Name: "b", RequiredCPU: 2, RequiredMemory: 2},
				}}},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a"}, cpu: 1, memory: 1, copies: 1},
					{services: []string{"b"}, cpu: 2, memory: 2, copies: 1},
				},
			),
			Entry("keeps independent RunsWith groups separate",
				[]Workload{
					{Name: "one", Count: 1, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 2, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
					{Name: "three", Count: 3, Services: []Service{{Name: "c", RequiredCPU: 3, RequiredMemory: 3, RunsWith: []string{"d"}}}},
					{Name: "four", Count: 4, Services: []Service{{Name: "d", RequiredCPU: 4, RequiredMemory: 4}}},
				},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a", "b"}, cpu: 3, memory: 3, copies: 2},
					{services: []string{"c", "d"}, cpu: 7, memory: 7, copies: 4},
				},
			),
		)
	})

	When("services declare Avoid links", func() {
		DescribeTable("splits conflicting services into separate bundles",
			func(workloads []Workload, machineSets []MachineSet, expected []bundleExpectation) {
				// Arrange
				original := cloneWorkloads(workloads)

				// Act
				bundles := NewSizer().createPlacementBundles(workloads, machineSets)

				// Assert
				Expect(workloads).To(Equal(original))
				Expect(snapshotBundles(bundles)).To(Equal(expectedSnapshots(expected)))
			},
			Entry("lets Avoid win over RunsWith in either input order",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"worker"}, Services: []Service{
						{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}},
						{Name: "c", RequiredCPU: 2, RequiredMemory: 2, Avoid: []string{"d"}},
					}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"database"}, Services: []Service{
						{Name: "b", RequiredCPU: 3, RequiredMemory: 3, Avoid: []string{"a"}},
						{Name: "d", RequiredCPU: 4, RequiredMemory: 4, RunsWith: []string{"c"}},
					}},
				},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a"}, allowed: []string{"worker"}, cpu: 1, memory: 1, copies: 2},
					{services: []string{"c"}, allowed: []string{"worker"}, cpu: 2, memory: 2, copies: 2},
					{services: []string{"b"}, allowed: []string{"database"}, cpu: 3, memory: 3, copies: 3},
					{services: []string{"d"}, allowed: []string{"database"}, cpu: 4, memory: 4, copies: 3},
				},
			),
			Entry("keeps Avoid conflicts in one workload in separate bundles without extra copies",
				[]Workload{{Name: "one", Count: 2, Services: []Service{
					{Name: "a", RequiredCPU: 1, RequiredMemory: 1, Avoid: []string{"b"}},
					{Name: "b", RequiredCPU: 2, RequiredMemory: 2, Avoid: []string{"a"}},
				}}},
				[]MachineSet(nil),
				[]bundleExpectation{
					{services: []string{"a"}, cpu: 1, memory: 1, copies: 2},
					{services: []string{"b"}, cpu: 2, memory: 2, copies: 2},
				},
			),
			Entry("splits an Avoid conflict after a RunsWith merge and restores origin metadata",
				[]Workload{
					{Name: "one", Count: 3, AllowedMachineSets: []string{"worker"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 2, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 2, AllowedMachineSets: []string{"cache"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 4, RunsWith: []string{"c"}}}},
					{Name: "three", Count: 1, AllowedMachineSets: []string{"database"}, Services: []Service{{Name: "c", RequiredCPU: 4, RequiredMemory: 8, Avoid: []string{"a"}}}},
				},
				[]MachineSet{{Name: "worker", CPU: 8}, {Name: "cache", CPU: 16}, {Name: "database", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"cache", "database", "worker"}, cpu: 3, memory: 6, copies: 3},
					{services: []string{"c"}, allowed: []string{"database"}, cpu: 4, memory: 8, copies: 1},
				},
			),
		)
	})

	When("RunsWith peers belong to machine-set-restricted bundles", func() {
		DescribeTable("chooses the bundle that survives the join",
			func(workloads []Workload, machineSets []MachineSet, expected []bundleExpectation) {
				// Arrange
				original := cloneWorkloads(workloads)

				// Act
				bundles := NewSizer().createPlacementBundles(workloads, machineSets)

				// Assert
				Expect(workloads).To(Equal(original))
				Expect(snapshotBundles(bundles)).To(Equal(expectedSnapshots(expected)))
			},
			Entry("prefers an unrestricted peer machine-set list",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"large"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 2, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 4}}},
				},
				[]MachineSet{{Name: "large", CPU: 64}, {Name: "small", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, cpu: 3, memory: 6, copies: 3},
				},
			),
			Entry("keeps an unrestricted current machine-set list",
				[]Workload{
					{Name: "one", Count: 2, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 2, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"large"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 4}}},
				},
				[]MachineSet{{Name: "large", CPU: 64}, {Name: "small", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, cpu: 3, memory: 6, copies: 3},
				},
			),
			Entry("prefers the longer peer machine-set list over CPU capacity",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"large"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"small", "medium"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
				},
				[]MachineSet{{Name: "large", CPU: 64}, {Name: "small", CPU: 8}, {Name: "medium", CPU: 16}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"small", "medium", "large"}, cpu: 3, memory: 3, copies: 3},
				},
			),
			Entry("keeps the longer current machine-set list",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"small", "medium"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"large"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
				},
				[]MachineSet{{Name: "large", CPU: 64}, {Name: "small", CPU: 8}, {Name: "medium", CPU: 16}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"small", "medium", "large"}, cpu: 3, memory: 3, copies: 3},
				},
			),
			Entry("prefers higher maximum CPU when machine-set list lengths tie",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"small"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"large"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
				},
				[]MachineSet{{Name: "small", CPU: 8}, {Name: "large", CPU: 64}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"large", "small"}, cpu: 3, memory: 3, copies: 3},
				},
			),
			Entry("keeps the current bundle when maximum CPU ties",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"small"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"medium"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
				},
				[]MachineSet{{Name: "small", CPU: 16}, {Name: "medium", CPU: 16}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"small", "medium"}, cpu: 3, memory: 3, copies: 3},
				},
			),
			Entry("ignores unknown machine sets when comparing CPU",
				[]Workload{
					{Name: "one", Count: 2, AllowedMachineSets: []string{"missing"}, Services: []Service{{Name: "a", RequiredCPU: 1, RequiredMemory: 1, RunsWith: []string{"b"}}}},
					{Name: "two", Count: 3, AllowedMachineSets: []string{"small"}, Services: []Service{{Name: "b", RequiredCPU: 2, RequiredMemory: 2}}},
				},
				[]MachineSet{{Name: "small", CPU: 8}},
				[]bundleExpectation{
					{services: []string{"a", "b"}, allowed: []string{"small", "missing"}, cpu: 3, memory: 3, copies: 3},
				},
			),
		)
	})

	When("callers retain the original slices", func() {
		It("keeps bundle state independent from caller-owned and per-call slices", func() {
			// Arrange
			workloads := []Workload{{
				Name:               "apps",
				Count:              2,
				AllowedMachineSets: []string{"worker"},
				Services: []Service{{
					Name:     "api",
					RunsWith: []string{"ghost"},
					Avoid:    []string{"ghost"},
				}},
			}}

			bundles := NewSizer().createPlacementBundles(workloads, nil)
			Expect(bundles).To(HaveLen(1))
			Expect(bundles[0].Count).To(Equal(2))

			// Act
			bundles[0].AllowedMachineSets[0] = "changed"

			again := NewSizer().createPlacementBundles(workloads, nil)
			Expect(again).To(HaveLen(1))
			Expect(again[0].AllowedMachineSets).To(Equal([]string{"worker"}))

			workloads[0].AllowedMachineSets[0] = "mutated"
			workloads[0].Services[0].RunsWith[0] = "mutated"
			workloads[0].Services[0].Avoid[0] = "mutated"

			// Assert
			expectedServices := []Service{{Name: "api", RunsWith: []string{"ghost"}, Avoid: []string{"ghost"}}}
			Expect(bundles[0].Services()).To(Equal(expectedServices))
			Expect(again[0].Services()).To(Equal(expectedServices))
		})
	})
})
