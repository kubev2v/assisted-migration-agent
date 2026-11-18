package models

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
