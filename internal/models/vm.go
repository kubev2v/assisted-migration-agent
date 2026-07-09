package models

// VirtualMachineSummary represents a lightweight VM record for list views.
type VirtualMachineSummary struct {
	ID                     string
	Name                   string
	PowerState             string
	Cluster                string
	Datacenter             string
	Memory                 int32 // MB
	DiskSize               int64 // MB (stored as MiB in DB, treated as MB)
	IssueCount             int
	IsMigratable           bool
	IsTemplate             bool
	InspectionStatus       InspectionStatus
	InspectionConcernCount int
	Groups                 []string
	UtilizationCpuP95      *float64 // CPU utilization at p95 (%); nil when no utilization data
	UtilizationMemP95      *float64 // Memory utilization at p95 (%); nil when no utilization data
	UtilizationCpuMax      *float64 // CPU utilization at max (%); nil when no utilization data
	UtilizationMemMax      *float64 // Memory utilization at max (%); nil when no utilization data
	UtilizationDisk        *float64 // Disk utilization (%); nil when no utilization data
	UtilizationConfidence  *float64 // Data confidence (%); nil when no utilization data
	MigrationExcluded      bool
	Labels                 []string
}

type VM struct {
	ID              string
	Name            string
	UUID            string
	Firmware        string
	PowerState      string
	ConnectionState string
	Host            string
	Folder          string
	Datacenter      string
	Cluster         string

	CpuCount       int32
	CoresPerSocket int32
	CpuAffinity    []int32
	MemoryMB       int32

	GuestName string
	GuestID   string
	HostName  string
	IPAddress string

	DiskSize    int64 // total disk size in MB (for list view)
	StorageUsed int64

	IsTemplate            bool
	IsMigratable          bool
	MigrationExcluded     bool
	FaultToleranceEnabled bool
	NestedHVEnabled       bool

	ToolsStatus        string
	ToolsRunningStatus string

	Disks         []Disk
	NICs          []NIC
	Devices       []Device
	GuestNetworks []GuestNetwork

	Issues []Issue

	Utilization *VmUtilizationDetails

	InspectionState    string
	InspectionError    string
	InspectionConcerns []VmInspectionConcern

	Labels []string
	Groups []string

	GuestApps []GuestApp
}

type GuestApp struct {
	Name    string
	Version string
}

type Issue struct {
	ID          string
	Label       string
	Description string
	Category    string
}

type Disk struct {
	Key      int32
	File     string
	Capacity int64
	Shared   bool
	RDM      bool
	Bus      string
	Mode     string
}

type NIC struct {
	MAC         string
	Network     string
	Index       int
	IPv4Address string
	IPv6Address string
}

type Device struct {
	Kind string
}

type GuestNetwork struct {
	Device       string
	MAC          string
	IP           string
	PrefixLength int32
	Network      string
}

// VMFilterOptions holds the distinct values available for filtering VMs.
type VMFilterOptions struct {
	Clusters          []string
	Datacenters       []string
	ConcernLabels     []string
	ConcernCategories []string
	Applications      []string
}

// Folder represents a VM folder in the vCenter hierarchy.
type Folder struct {
	ID   string
	Name string
}

// BatchOperationResult represents the result of a batch operation on VMs.
type BatchOperationResult struct {
	Results []OperationResult
}

// OperationResult represents the result of an operation on a single VM.
type OperationResult struct {
	VMID  string
	Error error
}

// Succeeded returns the count of successful operations.
func (r *BatchOperationResult) Succeeded() int {
	count := 0
	for _, result := range r.Results {
		if result.Error == nil {
			count++
		}
	}
	return count
}

// Failed returns the count of failed operations.
func (r *BatchOperationResult) Failed() int {
	count := 0
	for _, result := range r.Results {
		if result.Error != nil {
			count++
		}
	}
	return count
}

// Failures returns only the failed operation results.
func (r *BatchOperationResult) Failures() []OperationResult {
	var failures []OperationResult
	for _, result := range r.Results {
		if result.Error != nil {
			failures = append(failures, result)
		}
	}
	return failures
}
