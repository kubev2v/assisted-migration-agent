package v1_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("Sizing API conversions", func() {
	It("preserves required values and leaves omitted sizing options unset", func() {
		var req v1.ClusterRequirementsRequest
		Expect(json.Unmarshal([]byte(`{
			"cpuOverCommitRatio":"1:4", "memoryOverCommitRatio":"1:2",
			"workerNodeCPU":32, "workerNodeMemory":128
		}`), &req)).To(Succeed())

		Expect(req.ToModel()).To(Equal(models.ClusterRequirementsRequestForm{
			CPUOverCommitMultiplier: 4, MemoryOverCommitMultiplier: 2,
			WorkerNodeCPU: 32, WorkerNodeMemory: 128,
		}))
	})

	It("preserves the filter and explicit optional false and zero values without validation", func() {
		var req v1.ClusterRequirementsRequest
		Expect(json.Unmarshal([]byte(`{
			"filterExpression":"cluster = 'production'",
			"cpuOverCommitRatio":"1:1", "memoryOverCommitRatio":"1:4",
			"workerNodeCPU":16, "workerNodeMemory":64,
			"workerNodeThreads":0, "controlPlaneSchedulable":false,
			"controlPlaneNodeCount":1, "controlPlaneCPU":0, "controlPlaneMemory":0,
			"hostedControlPlane":false, "compactMode":false
		}`), &req)).To(Succeed())
		zero, one, disabled := 0, 1, false

		Expect(req.ToModel()).To(Equal(models.ClusterRequirementsRequestForm{
			Expression:              "cluster = 'production'",
			CPUOverCommitMultiplier: 1, MemoryOverCommitMultiplier: 4,
			WorkerNodeCPU: 16, WorkerNodeMemory: 64,
			WorkerNodeThreads: &zero, ControlPlaneSchedulable: &disabled,
			ControlPlaneNodeCount: &one, ControlPlaneCPU: &zero, ControlPlaneMemory: &zero,
			HostedControlPlane: &disabled, CompactMode: &disabled,
		}))
	})

	It("does not supply missing required ratios during conversion", func() {
		form := (v1.ClusterRequirementsRequest{}).ToModel()
		Expect(form.CPUOverCommitMultiplier).To(BeZero())
		Expect(form.MemoryOverCommitMultiplier).To(BeZero())
	})

	It("maps the sizing response and omits absent optional objects", func() {
		result := models.ClusterRequirementsResponse{
			ClusterSizing: models.ClusterSizing{
				TotalNodes: 8, WorkerNodes: 4, ControlPlaneNodes: 3,
				FailoverNodes: 1, TotalCPU: 160, TotalMemory: 640,
			},
			ResourceConsumption: models.SizingResourceConsumption{CPU: 120.5, Memory: 400.25},
			InventoryTotals:     models.InventoryTotals{TotalVMs: 20, TotalCPU: 100, TotalMemory: 300},
		}

		data, err := json.Marshal(v1.NewClusterRequirementsResponseFromModel(result))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(MatchJSON(`{
			"clusterSizing":{"totalNodes":8,"workerNodes":4,"controlPlaneNodes":3,
				"failoverNodes":1,"totalCPU":160,"totalMemory":640},
			"resourceConsumption":{"cpu":120.5,"memory":400.25},
			"inventoryTotals":{"totalVMs":20,"totalCPU":100,"totalMemory":300}
		}`))
	})

	It("maps optimization results and optional consumption details", func() {
		cpuMax, memoryMax, confidence := 85.5, 75.25, 95.0
		result := models.ClusterRequirementsResponse{
			OptimizedSizing: &models.ClusterSizing{
				TotalNodes: 5, WorkerNodes: 1, ControlPlaneNodes: 3,
				FailoverNodes: 1, TotalCPU: 96, TotalMemory: 384,
				CPUUtilizationMax: &cpuMax, MemoryUtilizationMax: &memoryMax, Confidence: &confidence,
			},
			Savings:            &models.Savings{NodesSaved: 3, PercentageReduction: 37.5, Description: "utilization"},
			OptimizationStatus: &models.OptimizationStatus{Attempted: true, Reason: "success"},
			ResourceConsumption: models.SizingResourceConsumption{
				Limits:          &models.SizingResourceLimits{CPU: 100, Memory: 200},
				OverCommitRatio: &models.SizingOverCommitRatio{CPU: 4, Memory: 2},
			},
		}

		response := v1.NewClusterRequirementsResponseFromModel(result)
		data, err := json.Marshal(response.OptimizedSizing)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(MatchJSON(`{
			"totalNodes":5,"workerNodes":1,"controlPlaneNodes":3,"failoverNodes":1,
			"totalCPU":96,"totalMemory":384,"cpuUtilizationMax":85.5,
			"memoryUtilizationMax":75.25,"confidence":95
		}`))
		Expect(response.Savings).To(HaveValue(Equal(v1.Savings{
			NodesSaved: 3, PercentageReduction: 37.5, Description: "utilization",
		})))
		Expect(response.OptimizationStatus).To(HaveValue(Equal(v1.OptimizationStatus{
			Attempted: true, Reason: v1.Success,
		})))
		Expect(response.ResourceConsumption.Limits).To(HaveValue(Equal(v1.SizingResourceLimits{Cpu: 100, Memory: 200})))
		Expect(response.ResourceConsumption.OverCommitRatio).To(HaveValue(Equal(v1.SizingOverCommitRatio{Cpu: 4, Memory: 2})))
	})
})
