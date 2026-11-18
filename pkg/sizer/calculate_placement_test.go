package sizer

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// boolPointer returns an addressable bool for optional machine-set permissions.
func boolPointer(value bool) *bool {
	return &value
}

// controlPlaneRequest builds a request whose only eligible machine set is a control plane.
func controlPlaneRequest(permission *bool) ([]MachineSet, []Workload) {
	machineSets := []MachineSet{{
		Name:                          "cp",
		IsControlPlane:                true,
		CPU:                           8,
		Memory:                        32,
		AllowWorkloadScheduling:       permission,
		ControlPlaneReservedResources: &ControlPlaneResources{CPU: 1, Memory: 2},
	}}
	workloads := []Workload{{
		Count:              1,
		AllowedMachineSets: []string{"cp"},
		Services:           []Service{{Name: "app", RequiredCPU: 1, RequiredMemory: 2}},
	}}

	return machineSets, workloads
}

var _ = Describe("Calculate workload placement", func() {
	When("workloads provide services and copy counts", func() {
		It("joins RunsWith peers, keeps Avoid peers apart, and skips disabled workloads", func() {
			// Arrange
			machineSets := []MachineSet{{Name: "worker", CPU: 16, Memory: 64}}
			workloads := []Workload{
				{
					Name:  "first",
					Count: 2,
					Services: []Service{
						{Name: "a", RequiredCPU: 1, RunsWith: []string{"b"}},
						{Name: "split", RequiredCPU: 2, Avoid: []string{"a"}},
					},
				},
				{Name: "second", Count: 3, Services: []Service{{Name: "b", RequiredCPU: 3}}},
				{
					Name:  "disabled",
					Count: 0,
					Services: []Service{
						{Name: "disabled", RequiredCPU: 1000, RunsWith: []string{"a"}},
					},
				},
			}

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			counts := map[string]int{}
			for _, node := range result.Nodes {
				placed := map[string]bool{}
				for _, service := range node.Services {
					counts[service.Name]++
					placed[service.Name] = true
				}
				Expect(placed["a"]).To(Equal(placed["b"]), "RunsWith peers must share a node")
				Expect(placed["a"] && placed["split"]).To(BeFalse(), "Avoid peers must not share a node")
			}

			Expect(counts).To(Equal(map[string]int{"a": 3, "b": 3, "split": 2}))
			Expect(result.NodeCount).To(Equal(5))
			Expect(result.ResourceConsumption.CPU).To(Equal(float64(16)))
		})

		DescribeTable("produces no placement for non-positive copy counts",
			func(count int) {
				// Arrange
				machineSets := []MachineSet{{Name: "worker", CPU: 16, Memory: 64}}
				workloads := []Workload{{
					Name:     "disabled",
					Count:    count,
					Services: []Service{{Name: "disabled", RequiredCPU: 1000, RunsWith: []string{"a"}}},
				}}

				// Act
				result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result.NodeCount).To(BeZero())
				Expect(result.Nodes).To(BeEmpty())
			},
			Entry("when the count is zero", 0),
			Entry("when the count is negative", -1),
		)
	})

	When("the only eligible machine set is a control plane", func() {
		DescribeTable("rejects workloads without scheduling permission",
			func(permission *bool) {
				// Arrange
				machineSets, workloads := controlPlaneRequest(permission)

				// Act
				result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

				// Assert
				Expect(err).To(MatchError("machine sets [cp] do not allow workload scheduling"))
				Expect(result).To(BeNil())
			},
			Entry("when permission is unset", nil),
			Entry("when permission is false", boolPointer(false)),
		)

		It("permits placement when scheduling is enabled", func() {
			// Arrange
			machineSets, workloads := controlPlaneRequest(boolPointer(true))

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(1))
			Expect(result.Nodes[0].CPU.Requested).To(Equal(float64(1)))
		})

		It("rejects an all-control-plane cluster even when the allowed list is empty", func() {
			// Arrange
			machineSets, workloads := controlPlaneRequest(nil)
			workloads[0].AllowedMachineSets = nil

			// Act
			_, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

			// Assert
			Expect(err).To(MatchError("no machine set allows workload scheduling"))
		})
	})

	When("the allowed machine sets do not exist", func() {
		It("reports a placement error naming the unknown sets", func() {
			// Arrange
			machineSets := []MachineSet{{Name: "worker", CPU: 8, Memory: 32}}
			workloads := []Workload{{
				Count:              1,
				AllowedMachineSets: []string{"missing"},
				Services:           []Service{{Name: "app", RequiredCPU: 1, RequiredMemory: 2}},
			}}

			// Act
			_, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

			// Assert
			Expect(err).To(MatchError(`allowed machine set "missing" does not exist`))
		})
	})

	When("a permitted machine set is too small", func() {
		It("reports a resource error despite a closed control plane in the allowed list", func() {
			// Arrange
			closed := false
			machineSets := []MachineSet{
				{Name: "cp", IsControlPlane: true, AllowWorkloadScheduling: &closed, CPU: 8, Memory: 32},
				{Name: "small", CPU: 2, Memory: 4},
			}
			workloads := []Workload{{
				Count:              1,
				AllowedMachineSets: []string{"cp", "small"},
				Services:           []Service{{Name: "app", RequiredCPU: 4, RequiredMemory: 8}},
			}}

			// Act
			_, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

			// Assert
			var resourceErr *NotEnoughResourcesError
			Expect(errors.As(err, &resourceErr)).To(BeTrue())
		})
	})

	When("machine sets differ in capacity", func() {
		It("creates the largest fitting node and keeps Avoid peers apart", func() {
			// Arrange
			machineSets := []MachineSet{
				{Name: "small", CPU: 2, Memory: 8},
				{Name: "large", CPU: 8, Memory: 32},
			}
			workloads := []Workload{
				{Count: 1, Services: []Service{{Name: "a", RequiredCPU: 2, Avoid: []string{"b"}}}},
				{Count: 1, Services: []Service{{Name: "b", RequiredCPU: 1}}},
				{Count: 1, Services: []Service{{Name: "c", RequiredCPU: 1, LimitCPU: 100}}},
			}

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result.NodeCount).To(Equal(2))
			Expect(result.Nodes[0].MachineSetName).To(Equal("large"))
			Expect(result.Nodes[0].Services).To(HaveLen(2))
		})

		It("reports NotEnoughResourcesError when joined services exceed every machine set", func() {
			// Arrange
			machineSets := []MachineSet{
				{Name: "small", CPU: 2, Memory: 8},
				{Name: "large", CPU: 8, Memory: 32},
			}
			workloads := []Workload{{
				Count: 1,
				Services: []Service{
					{Name: "a", RequiredCPU: 4, RunsWith: []string{"b"}},
					{Name: "b", RequiredCPU: 4},
				},
			}}

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

			// Assert
			var resourceErr *NotEnoughResourcesError
			Expect(errors.As(err, &resourceErr)).To(BeTrue())
			Expect(result).To(BeNil())
		})
	})

	When("two machine sets share a name", func() {
		It("rejects the ambiguous template and permission lookups", func() {
			// Arrange
			machineSets := []MachineSet{
				{Name: "worker", CPU: 8, Memory: 32},
				{Name: "worker", IsControlPlane: true, CPU: 16, Memory: 64},
			}
			workloads := []Workload{{Count: 1, Services: []Service{{Name: "app", RequiredCPU: 1}}}}

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

			// Assert
			Expect(err).To(MatchError(`duplicate machine set name "worker"`))
			Expect(result).To(BeNil())
		})
	})

	When("service names repeat across workloads", func() {
		DescribeTable("rejects the ambiguous RunsWith and Avoid references",
			func(workloads []Workload) {
				// Arrange
				machineSets := []MachineSet{{Name: "worker", CPU: 8, Memory: 32}}

				// Act
				result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

				// Assert
				Expect(err).To(MatchError(`duplicate service name "app"`))
				Expect(result).To(BeNil())
			},
			Entry("when two active workloads define the same service", []Workload{
				{Name: "one", Count: 1, Services: []Service{{Name: "app", RequiredCPU: 1}}},
				{Name: "two", Count: 2, Services: []Service{{Name: "app", RequiredCPU: 2}}},
			}),
			Entry("when a zero-count workload defines the same service", []Workload{
				{Name: "one", Count: 1, Services: []Service{{Name: "app", RequiredCPU: 1}}},
				{Name: "disabled", Count: 0, Services: []Service{{Name: "app", RequiredCPU: 2}}},
			}),
			Entry("when one workload defines the same service twice", []Workload{
				{Name: "apps", Count: 1, Services: []Service{
					{Name: "app", RequiredCPU: 1},
					{Name: "app", RequiredCPU: 2},
				}},
			}),
			Entry("when a RunsWith reference would be ambiguous", []Workload{
				{Name: "one", Count: 1, Services: []Service{
					{Name: "app", RequiredCPU: 1},
					{Name: "app", RequiredCPU: 2},
				}},
				{Name: "two", Count: 1, Services: []Service{{Name: "b", RequiredCPU: 3, RunsWith: []string{"app"}}}},
			}),
		)
	})

	When("Avoid and RunsWith link the same service pair", func() {
		DescribeTable("keeps the pair apart regardless of declaration order",
			func(services []Service) {
				// Arrange
				machineSets := []MachineSet{{Name: "worker", CPU: 8, Memory: 32}}
				workloads := []Workload{{Count: 1, Services: services}}

				// Act
				result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(result.NodeCount).To(Equal(2))
			},
			Entry("when Avoid is declared first", []Service{
				{Name: "a", RequiredCPU: 1, Avoid: []string{"b"}},
				{Name: "b", RequiredCPU: 1, RunsWith: []string{"a"}},
			}),
			Entry("when RunsWith is declared first", []Service{
				{Name: "b", RequiredCPU: 1, RunsWith: []string{"a"}},
				{Name: "a", RequiredCPU: 1, Avoid: []string{"b"}},
			}),
		)
	})
})
