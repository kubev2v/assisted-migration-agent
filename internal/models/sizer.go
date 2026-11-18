package models

import (
	"errors"
)

const (
	OptimizationNoUtilizationData = "no_utilization_data"
	OptimizationLowConfidence     = "low_confidence"
	OptimizationCalculationError  = "calculation_error"
	OptimizationSuccess           = "success"
)

const (
	defaultControlPlaneCPU         = 6
	defaultControlPlaneMemory      = 16
	defaultControlPlaneNodeCount   = 3
	defaultControlPlaneSchedulable = false
	defaultHostedControlPlane      = false
	defaultCompactMode             = false
)

type ClusterRequirementsRequestForm struct {
	Expression                 string
	CPUOverCommitMultiplier    float64
	MemoryOverCommitMultiplier float64
	WorkerNodeCPU              int
	WorkerNodeMemory           int

	WorkerNodeThreads       *int
	ControlPlaneSchedulable *bool
	ControlPlaneNodeCount   *int
	ControlPlaneCPU         *int
	ControlPlaneMemory      *int
	HostedControlPlane      *bool
	CompactMode             *bool
}

type ClusterRequirementsResponse struct {
	ClusterSizing       ClusterSizing
	ResourceConsumption SizingResourceConsumption
	InventoryTotals     InventoryTotals
	OptimizedSizing     *ClusterSizing
	Savings             *Savings
	OptimizationStatus  *OptimizationStatus
}

type ClusterSizing struct {
	TotalNodes           int
	WorkerNodes          int
	ControlPlaneNodes    int
	FailoverNodes        int
	TotalCPU             int
	TotalMemory          int
	CPUUtilizationMax    *float64
	MemoryUtilizationMax *float64
	Confidence           *float64
}

type SizingResourceConsumption struct {
	CPU             float64
	Memory          float64
	Limits          *SizingResourceLimits
	OverCommitRatio *SizingOverCommitRatio
}

type SizingResourceLimits struct {
	CPU    float64
	Memory float64
}

type SizingOverCommitRatio struct {
	CPU    float64
	Memory float64
}

type InventoryTotals struct {
	TotalVMs    int
	TotalCPU    int
	TotalMemory int
}

type Savings struct {
	NodesSaved          int
	PercentageReduction float64
	Description         string
}

type OptimizationStatus struct {
	Attempted bool
	Reason    string
}

func (form ClusterRequirementsRequestForm) Validate() error {
	if form.WorkerNodeCPU < 2 || form.WorkerNodeCPU > 384 {
		return errors.New("workerNodeCPU must be between 2 and 384")
	}

	if form.WorkerNodeMemory < 4 || form.WorkerNodeMemory > 4096 {
		return errors.New("workerNodeMemory must be between 4 and 4096 GB")
	}

	if form.WorkerNodeThreads != nil {
		threads := *form.WorkerNodeThreads
		if threads < 2 || threads > 2000 {
			return errors.New("workerNodeThreads must be between 2 and 2000")
		}

		if threads < form.WorkerNodeCPU {
			return errors.New("workerNodeThreads must be greater than or equal to workerNodeCPU")
		}
	}

	hostedControlPlane := form.HostedControlPlane != nil && *form.HostedControlPlane
	if hostedControlPlane {
		if form.ControlPlaneNodeCount != nil {
			return errors.New("controlPlaneNodeCount cannot be specified when hostedControlPlane is true")
		}

		if form.ControlPlaneCPU != nil {
			return errors.New("controlPlaneCPU cannot be specified when hostedControlPlane is true")
		}

		if form.ControlPlaneMemory != nil {
			return errors.New("controlPlaneMemory cannot be specified when hostedControlPlane is true")
		}

		if form.ControlPlaneSchedulable != nil {
			return errors.New("controlPlaneSchedulable cannot be specified when hostedControlPlane is true")
		}
	}

	if form.ControlPlaneCPU != nil && (*form.ControlPlaneCPU < 2 || *form.ControlPlaneCPU > 384) {
		return errors.New("controlPlaneCPU must be between 2 and 384")
	}

	if form.ControlPlaneMemory != nil && (*form.ControlPlaneMemory < 4 || *form.ControlPlaneMemory > 4096) {
		return errors.New("controlPlaneMemory must be between 4 and 4096 GB")
	}

	if form.ControlPlaneNodeCount != nil && *form.ControlPlaneNodeCount != 1 && *form.ControlPlaneNodeCount != 3 {
		return errors.New("controlPlaneNodeCount must be 1 or 3")
	}

	if form.CompactMode != nil && *form.CompactMode {
		if hostedControlPlane {
			return errors.New("compactMode cannot be true when hostedControlPlane is true")
		}

		if form.ControlPlaneNodeCount == nil || *form.ControlPlaneNodeCount != 3 {
			return errors.New("compactMode requires controlPlaneNodeCount to be 3")
		}

		if form.ControlPlaneSchedulable == nil || !*form.ControlPlaneSchedulable {
			return errors.New("compactMode requires controlPlaneSchedulable to be true")
		}
	}

	if form.ControlPlaneNodeCount != nil && *form.ControlPlaneNodeCount == 1 {
		if form.ControlPlaneSchedulable == nil || !*form.ControlPlaneSchedulable {
			return errors.New("single-node clusters require controlPlaneSchedulable to be true")
		}
	}

	return nil
}

func (form ClusterRequirementsRequestForm) WithDefaults() ClusterRequirementsRequestForm {
	if form.WorkerNodeThreads == nil {
		threads := form.WorkerNodeCPU
		form.WorkerNodeThreads = &threads
	}

	if form.HostedControlPlane == nil {
		hosted := defaultHostedControlPlane
		form.HostedControlPlane = &hosted
	}

	if form.CompactMode == nil {
		compact := defaultCompactMode
		form.CompactMode = &compact
	}

	if !*form.HostedControlPlane {
		if form.ControlPlaneNodeCount == nil {
			count := defaultControlPlaneNodeCount
			form.ControlPlaneNodeCount = &count
		}

		if form.ControlPlaneCPU == nil {
			cpu := defaultControlPlaneCPU
			form.ControlPlaneCPU = &cpu
		}

		if form.ControlPlaneMemory == nil {
			memory := defaultControlPlaneMemory
			form.ControlPlaneMemory = &memory
		}

		if form.ControlPlaneSchedulable == nil {
			schedulable := defaultControlPlaneSchedulable
			form.ControlPlaneSchedulable = &schedulable
		}
	}

	return form
}
