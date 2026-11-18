package sizer

type MachineSet struct {
	Name string
	// IsControlPlane identifies nodes in this machine set as control-plane nodes.
	IsControlPlane bool
	// CPU is the physical CPU capacity of each node in this machine set.
	CPU int
	// Memory is the physical memory capacity of each node, in GB.
	Memory                        int
	InstanceName                  string
	NumberOfDisks                 int
	Label                         string
	AllowWorkloadScheduling       *bool
	ControlPlaneReservedResources *ControlPlaneResources
}

type ControlPlaneResources struct {
	// CPU and Memory are added to the requested baseline of a new control-plane node.
	CPU    float64
	Memory float64
}

type Workload struct {
	Name string
	// Count is the number of copies; non-positive values ignore this workload.
	Count int
	// AllowedMachineSets lists the machine sets whose nodes may host this workload's services.
	// Each machine set is a template for creating nodes.
	AllowedMachineSets []string
	Services           []Service
}

type Service struct {
	Name string
	// RequiredCPU is the CPU request used to decide whether this service fits on a node.
	RequiredCPU float64
	// RequiredMemory is the memory request in GB used for placement.
	RequiredMemory float64
	// LimitCPU is the configured CPU size used for reporting, not placement; zero uses RequiredCPU.
	LimitCPU float64
	// LimitMemory is the configured memory size in GB used for reporting; zero uses RequiredMemory.
	LimitMemory float64
	RunsWith    []string
	Avoid       []string
}

type SizerResponse struct {
	NodeCount int
	// Nodes holds per-node details when the request asks for them.
	Nodes []Node
	// TotalCPU is the sum of physical CPU capacity across placed nodes.
	TotalCPU int
	// TotalMemory is the sum of physical memory capacity across placed nodes, in GB.
	TotalMemory         int
	ResourceConsumption ResourceConsumption
}

type ResourceConsumption struct {
	// CPU and Memory sum placed services' requests, before kubelet reservations.
	CPU    float64
	Memory float64
	// Limits sums configured service sizes when any placed service has a nonzero limit.
	Limits *ResourceLimits
	// OverCommitRatio compares limits with physical capacity after kubelet reservations.
	OverCommitRatio *OverCommitRatio
}

type ResourceLimits struct {
	// CPU and Memory sum configured sizes; a missing service limit contributes its request.
	CPU    float64
	Memory float64
}

type OverCommitRatio struct {
	// CPU and Memory are reported limits divided by available node capacity, not input multipliers.
	CPU    float64
	Memory float64
}

type Node struct {
	Name           string
	MachineSetName string
	IsControlPlane bool
	CPU            Resource
	Memory         Resource
	Services       []Service
}

type Resource struct {
	// Requested includes reservations in scheduler state; response details report service requests only.
	Requested float64
	// Total is this node's physical CPU or memory capacity.
	Total int
	// Limits sums configured service sizes on this node for reporting.
	Limits float64
	// OverCommitRatio is Limits divided by this node's capacity after kubelet reservation.
	OverCommitRatio float64
}
