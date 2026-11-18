package sizer

import (
	"context"
	"encoding/json"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// calculateForTest records an independent concurrent result so specs can detect shared mutable state.
func calculateForTest(group *sync.WaitGroup, sizer *Sizer, machineSets []MachineSet, workloads []Workload, detailed bool, results []*SizerResponse, failures []error, index int) {
	defer group.Done()
	results[index], failures[index] = sizer.Calculate(context.Background(), machineSets, workloads, detailed)
}

var _ = Describe("Calculate", func() {
	When("the request is incomplete", func() {
		DescribeTable("rejects it without returning a result",
			func(machineSets []MachineSet, workloads []Workload) {
				// Act
				result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, false)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			},
			Entry("when machine sets are missing", nil, []Workload{{Name: "vms"}}),
			Entry("when workloads are missing", []MachineSet{{Name: "worker"}}, nil),
		)
	})

	When("the same sizer serves concurrent requests", func() {
		It("returns identical results and leaves the caller-owned slices unchanged", func() {
			// Arrange
			machineSets := []MachineSet{{Name: "worker", CPU: 8, Memory: 32, NumberOfDisks: 24}}
			workloads := []Workload{{
				Name:     "vms",
				Count:    3,
				Services: []Service{{Name: "vm", RequiredCPU: 1, RequiredMemory: 2}},
			}}

			machineSetsSnapshot, err := json.Marshal(machineSets)
			Expect(err).NotTo(HaveOccurred())
			workloadsSnapshot, err := json.Marshal(workloads)
			Expect(err).NotTo(HaveOccurred())

			var machineSetsBefore []MachineSet
			Expect(json.Unmarshal(machineSetsSnapshot, &machineSetsBefore)).To(Succeed())
			var workloadsBefore []Workload
			Expect(json.Unmarshal(workloadsSnapshot, &workloadsBefore)).To(Succeed())

			sizer := NewSizer()
			expected, err := sizer.Calculate(context.Background(), machineSets, workloads, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(expected.NodeCount).To(Equal(3))
			Expect(expected.Nodes).To(HaveLen(3))

			// Act
			results := make([]*SizerResponse, 8)
			failures := make([]error, 8)
			var group sync.WaitGroup
			for index := range results {
				group.Add(1)
				go calculateForTest(&group, sizer, machineSets, workloads, true, results, failures, index)
			}
			group.Wait()

			// Assert
			for index := range results {
				Expect(failures[index]).NotTo(HaveOccurred())
				Expect(results[index]).To(Equal(expected))
			}
			Expect(machineSets).To(Equal(machineSetsBefore))
			Expect(workloads).To(Equal(workloadsBefore))
		})
	})

	When("resources are fractional", func() {
		It("reports them without losing float64 precision", func() {
			// Arrange
			machineSets := []MachineSet{{Name: "worker", CPU: 8, Memory: 32, NumberOfDisks: 24}}
			workloads := []Workload{{
				Name:  "fractional",
				Count: 1,
				Services: []Service{{
					Name:           "service",
					RequiredCPU:    .125,
					RequiredMemory: 1.005,
					LimitCPU:       2.675,
					LimitMemory:    3.125,
				}},
			}}

			// Act
			result, err := NewSizer().Calculate(context.Background(), machineSets, workloads, true)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			consumption := result.ResourceConsumption
			Expect(consumption.CPU).To(Equal(.125))
			Expect(consumption.Memory).To(Equal(1.005))
			Expect(consumption.Limits).NotTo(BeNil())
			Expect(consumption.Limits.CPU).To(Equal(2.675))
			Expect(consumption.Limits.Memory).To(Equal(3.125))

			cpuRatio := 2.675 / (8 - kubeletCPU(8))
			memoryRatio := 3.125 / (32 - kubeletMemory(32))
			Expect(consumption.OverCommitRatio).NotTo(BeNil())
			Expect(consumption.OverCommitRatio.CPU).To(Equal(cpuRatio))
			Expect(consumption.OverCommitRatio.Memory).To(Equal(memoryRatio))

			Expect(result.Nodes).To(HaveLen(1))
			node := result.Nodes[0]
			Expect(node.CPU.Requested).To(Equal(.125))
			Expect(node.Memory.Requested).To(Equal(1.005))
			Expect(node.CPU.Limits).To(Equal(2.675))
			Expect(node.Memory.Limits).To(Equal(3.125))
			Expect(node.CPU.OverCommitRatio).To(Equal(cpuRatio))
			Expect(node.Memory.OverCommitRatio).To(Equal(memoryRatio))
		})
	})
})

var _ = Describe("kubeletMemory", func() {
	DescribeTable("preserves the original reservation tiers",
		func(memory int, expected float64) {
			// Act
			reserved := kubeletMemory(memory)

			// Assert
			Expect(reserved).To(BeNumerically("~", expected, 1e-12))
		},
		Entry("below 1 GB", 0, .255),
		Entry("at 1 GB", 1, 0.0),
		Entry("at 4 GB", 4, .75),
		Entry("at 8 GB", 8, 1.8),
		Entry("at 16 GB", 16, 2.6),
		Entry("at 128 GB", 128, 9.32),
	)
})
