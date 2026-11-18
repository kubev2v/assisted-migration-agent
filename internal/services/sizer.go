package services

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kubev2v/migration-planner-common/sizer"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	workerMachineSetName           = "worker"
	controlPlaneMachineSetName     = "controlPlane"
	controlPlaneServiceName        = "ControlPlane"
	machineSetNumberOfDisks        = 24
	controlPlaneReservedCPU        = 3.5
	controlPlaneReservedMemory     = 13.39
	smtThreadCapacity              = 0.5
	minFailoverNodes               = 2
	failoverCapacityPercent        = 10.0
	maxNodeCount                   = 100
	maxVMsPerWorkerNode            = 200
	minSizingUtilizationConfidence = 50.0
)

type SizerService struct {
	store *store.Store
}

func NewSizerService(st *store.Store) *SizerService {
	return &SizerService{store: st}
}

func (s *SizerService) CalculateClusterRequirements(ctx context.Context, form models.ClusterRequirementsRequestForm) (*models.ClusterRequirementsResponse, error) {
	if err := form.Validate(); err != nil {
		return nil, srvErrors.NewValidationError(err.Error())
	}

	params := form.WithDefaults()

	vms, total, err := NewVMService(s.store).List(ctx, VMListParams{Expression: params.Expression})
	if err != nil {
		return nil, fmt.Errorf("listing VMs for sizing: %w", err)
	}

	if total == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no VMs available for sizing for the provided expression: %s", params.Expression))
	}

	workloads := make([]sizer.Workload, 0, len(vms))
	var totals models.InventoryTotals

	for _, vm := range vms {
		if vm.MigrationExcluded {
			continue
		}

		if vm.CpuCount < 0 || vm.Memory < 0 {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("VM %q must have non negative CPU and memory resources", vm.ID))
		}

		memoryGB := float64(vm.Memory) / 1024.0
		workloads = append(workloads, newVMWorkload(vm, params))

		totals.TotalVMs++
		totals.TotalCPU += int(vm.CpuCount)
		totals.TotalMemory += int(math.Round(memoryGB))
	}

	if totals.TotalVMs == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no VMs available for sizing for the provided expression: %s", params.Expression))
	}

	result, err := s.computeClusterRequirements(ctx, params, workloads)
	if err != nil {
		return nil, err
	}

	result.InventoryTotals = totals
	s.addOptimizedSizing(ctx, params, vms, result)

	return result, nil
}

func (s *SizerService) computeClusterRequirements(ctx context.Context, params models.ClusterRequirementsRequestForm, workloads []sizer.Workload) (*models.ClusterRequirementsResponse, error) {
	var sizing *sizer.SizerResponse
	var err error
	switch {
	case *params.HostedControlPlane:
		sizing, err = s.computeHostedControlPlane(ctx, params, workloads)
	case *params.ControlPlaneNodeCount == 1:
		sizing, err = s.computeSNO(ctx, params, workloads)
	default:
		sizing, err = s.computeHACluster(ctx, params, workloads)
	}
	if err != nil {
		return nil, err
	}

	if sizing.NodeCount > maxNodeCount {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("cluster requires %d nodes, exceeding the maximum of %d; use larger nodes", sizing.NodeCount, maxNodeCount))
	}

	controlPlaneNodeCount := 0
	if params.ControlPlaneNodeCount != nil {
		controlPlaneNodeCount = *params.ControlPlaneNodeCount
	}
	singleNode := controlPlaneNodeCount == 1

	var workerNodes, controlPlaneNodes int
	for _, node := range sizing.Nodes {
		if node.IsControlPlane {
			controlPlaneNodes++
			if !singleNode {
				continue
			}
		} else {
			workerNodes++
		}

		vmCount := 0
		for _, service := range node.Services {
			if service.Name != controlPlaneServiceName {
				vmCount++
			}
		}

		if vmCount > maxVMsPerWorkerNode {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("VM distribution constraint violated: found %d VMs on a node, exceeds limit of %d per node", vmCount, maxVMsPerWorkerNode))
		}
	}

	if controlPlaneNodes > controlPlaneNodeCount {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("workload requires %d control-plane nodes, but %d are configured; use larger worker or control-plane nodes", controlPlaneNodes, controlPlaneNodeCount))
	}

	failoverNodes := 0
	if workerNodes > 0 {
		failoverNodes = max(minFailoverNodes, int(math.Ceil(float64(workerNodes)*failoverCapacityPercent/100.0)))
	}

	result := &models.ClusterRequirementsResponse{
		ClusterSizing: models.ClusterSizing{
			TotalNodes:        controlPlaneNodes + workerNodes + failoverNodes,
			WorkerNodes:       workerNodes + failoverNodes,
			ControlPlaneNodes: controlPlaneNodes,
			FailoverNodes:     failoverNodes,
			TotalCPU:          sizing.TotalCPU,
			TotalMemory:       sizing.TotalMemory,
		},
		ResourceConsumption: models.SizingResourceConsumption{
			CPU:    sizing.ResourceConsumption.CPU,
			Memory: sizing.ResourceConsumption.Memory,
		},
	}

	if sizing.ResourceConsumption.Limits != nil {
		result.ResourceConsumption.Limits = &models.SizingResourceLimits{
			CPU:    sizing.ResourceConsumption.Limits.CPU,
			Memory: sizing.ResourceConsumption.Limits.Memory,
		}
	}

	if sizing.ResourceConsumption.OverCommitRatio != nil {
		result.ResourceConsumption.OverCommitRatio = &models.SizingOverCommitRatio{
			CPU:    sizing.ResourceConsumption.OverCommitRatio.CPU,
			Memory: sizing.ResourceConsumption.OverCommitRatio.Memory,
		}
	}

	return result, nil
}

func (s *SizerService) computeHostedControlPlane(ctx context.Context, params models.ClusterRequirementsRequestForm, workloads []sizer.Workload) (*sizer.SizerResponse, error) {
	machineSets := []sizer.MachineSet{newWorkerMachineSet(params)}
	for i := range workloads {
		workloads[i].AllowedMachineSets = []string{workerMachineSetName}
	}
	applyVMNodeLimit(workloads)

	sizing, err := sizer.NewSizer().Calculate(ctx, machineSets, workloads, true)
	if err != nil {
		var resourceErr *sizer.NotEnoughResourcesError
		if errors.As(err, &resourceErr) {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("configured nodes cannot fit a workload: %v", err))
		}
		return nil, fmt.Errorf("calculating cluster requirements: %w", err)
	}

	return sizing, nil
}

func (s *SizerService) computeSNO(ctx context.Context, params models.ClusterRequirementsRequestForm, workloads []sizer.Workload) (*sizer.SizerResponse, error) {
	machineSets := []sizer.MachineSet{newControlPlaneMachineSet(params)}
	for i := range workloads {
		workloads[i].AllowedMachineSets = []string{controlPlaneMachineSetName}
		// Binding every VM to the single control-plane service prevents extra nodes from being created.
		workloads[i].Services[0].RunsWith = []string{controlPlaneServiceName}
	}
	workloads = append([]sizer.Workload{newControlPlaneWorkload(*params.ControlPlaneNodeCount)}, workloads...)

	sizing, err := sizer.NewSizer().Calculate(ctx, machineSets, workloads, true)
	if err != nil {
		var resourceErr *sizer.NotEnoughResourcesError
		if errors.As(err, &resourceErr) {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("workload does not fit on a single control-plane node: %v", err))
		}
		return nil, fmt.Errorf("calculating cluster requirements: %w", err)
	}

	return sizing, nil
}

func (s *SizerService) computeHACluster(ctx context.Context, params models.ClusterRequirementsRequestForm, workloads []sizer.Workload) (*sizer.SizerResponse, error) {
	machineSets := make([]sizer.MachineSet, 0, 2)
	vmMachineSets := make([]string, 0, 2)

	if !*params.CompactMode {
		machineSets = append(machineSets, newWorkerMachineSet(params))
		vmMachineSets = append(vmMachineSets, workerMachineSetName)
	}

	machineSets = append(machineSets, newControlPlaneMachineSet(params))
	if *params.ControlPlaneSchedulable {
		vmMachineSets = append(vmMachineSets, controlPlaneMachineSetName)
	}

	for i := range workloads {
		workloads[i].AllowedMachineSets = vmMachineSets
	}
	applyVMNodeLimit(workloads)
	workloads = append([]sizer.Workload{newControlPlaneWorkload(*params.ControlPlaneNodeCount)}, workloads...)

	sizing, err := sizer.NewSizer().Calculate(ctx, machineSets, workloads, true)
	if err != nil {
		message := "configured nodes cannot fit a workload"
		if *params.CompactMode {
			message = "workload does not fit in compact mode"
		}

		var resourceErr *sizer.NotEnoughResourcesError
		if errors.As(err, &resourceErr) {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("%s: %v", message, err))
		}
		return nil, fmt.Errorf("calculating cluster requirements: %w", err)
	}

	if *params.CompactMode && sizing.NodeCount > *params.ControlPlaneNodeCount {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("workload does not fit in compact mode: requires %d nodes, maximum is %d", sizing.NodeCount, *params.ControlPlaneNodeCount))
	}

	return sizing, nil
}

func newVMWorkload(vm models.VirtualMachineSummary, params models.ClusterRequirementsRequestForm) sizer.Workload {
	cpu := float64(vm.CpuCount)
	memoryGB := float64(vm.Memory) / 1024.0

	return sizer.Workload{
		Name:  vm.ID,
		Count: 1,
		Services: []sizer.Service{{
			Name:           vm.ID,
			RequiredCPU:    cpu / params.CPUOverCommitMultiplier,
			RequiredMemory: memoryGB / params.MemoryOverCommitMultiplier,
			LimitCPU:       cpu,
			LimitMemory:    memoryGB,
		}},
	}
}

func newWorkerMachineSet(params models.ClusterRequirementsRequestForm) sizer.MachineSet {
	effectiveCPU := float64(params.WorkerNodeCPU)
	effectiveCPU += float64(*params.WorkerNodeThreads-params.WorkerNodeCPU) * smtThreadCapacity

	return sizer.MachineSet{
		Name:          workerMachineSetName,
		CPU:           int(math.Round(effectiveCPU)),
		Memory:        params.WorkerNodeMemory,
		InstanceName:  "bare-metal-worker",
		NumberOfDisks: machineSetNumberOfDisks,
		Label:         "Worker",
	}
}

func newControlPlaneMachineSet(params models.ClusterRequirementsRequestForm) sizer.MachineSet {
	allowControlPlaneWorkloads := true
	return sizer.MachineSet{
		Name:                    controlPlaneMachineSetName,
		IsControlPlane:          true,
		CPU:                     *params.ControlPlaneCPU,
		Memory:                  *params.ControlPlaneMemory,
		InstanceName:            "control-plane",
		NumberOfDisks:           machineSetNumberOfDisks,
		Label:                   "Control Plane",
		AllowWorkloadScheduling: &allowControlPlaneWorkloads,
	}
}

func newControlPlaneWorkload(nodeCount int) sizer.Workload {
	return sizer.Workload{
		Name:               "control-plane-services",
		Count:              nodeCount,
		AllowedMachineSets: []string{controlPlaneMachineSetName},
		Services: []sizer.Service{{
			Name:           controlPlaneServiceName,
			RequiredCPU:    controlPlaneReservedCPU,
			RequiredMemory: controlPlaneReservedMemory,
		}},
	}
}

func applyVMNodeLimit(workloads []sizer.Workload) {
	for i := range workloads {
		group := i / maxVMsPerWorkerNode
		for j := range workloads {
			if j/maxVMsPerWorkerNode != group {
				workloads[i].Services[0].Avoid = append(workloads[i].Services[0].Avoid, workloads[j].Services[0].Name)
			}
		}
	}
}

func (s *SizerService) addOptimizedSizing(ctx context.Context, params models.ClusterRequirementsRequestForm, vms []models.VirtualMachineSummary, result *models.ClusterRequirementsResponse) {
	result.OptimizationStatus = &models.OptimizationStatus{
		Reason: models.OptimizationNoUtilizationData,
	}

	workloads := make([]sizer.Workload, 0, len(vms))
	var totalCPU, totalMemory, optimizedCPU, optimizedMemory, weightedConfidence float64
	var hasUsableData, hasLowConfidence bool

	for _, vm := range vms {
		if vm.MigrationExcluded {
			continue
		}

		workload := newVMWorkload(vm, params)
		service := &workload.Services[0]
		totalCPU += service.LimitCPU
		totalMemory += service.LimitMemory

		confidence := 0.0
		if vm.UtilizationConfidence != nil && *vm.UtilizationConfidence > 0 &&
			!math.IsNaN(*vm.UtilizationConfidence) && !math.IsInf(*vm.UtilizationConfidence, 0) {
			confidence = *vm.UtilizationConfidence
			weightedConfidence += service.LimitCPU * confidence
		}

		if confidence > 0 && confidence < minSizingUtilizationConfidence {
			hasLowConfidence = true
		}

		if confidence >= minSizingUtilizationConfidence && vm.UtilizationCpuMax != nil && vm.UtilizationMemMax != nil {
			cpuPercent := *vm.UtilizationCpuMax
			memoryPercent := *vm.UtilizationMemMax
			if !math.IsNaN(cpuPercent) && !math.IsInf(cpuPercent, 0) && cpuPercent > 0 &&
				!math.IsNaN(memoryPercent) && !math.IsInf(memoryPercent, 0) && memoryPercent > 0 {
				cpuMultiplier := math.Min(cpuPercent/100.0, 1.0)
				memoryMultiplier := math.Min(memoryPercent/100.0, 1.0)
				service.RequiredCPU *= cpuMultiplier
				service.RequiredMemory *= memoryMultiplier
				service.LimitCPU *= cpuMultiplier
				service.LimitMemory *= memoryMultiplier
				hasUsableData = true
			}
		}

		optimizedCPU += service.LimitCPU
		optimizedMemory += service.LimitMemory
		workloads = append(workloads, workload)
	}

	if !hasUsableData {
		if hasLowConfidence {
			result.OptimizationStatus.Reason = models.OptimizationLowConfidence
		}
		return
	}

	if totalCPU == 0 || totalMemory == 0 {
		return
	}

	result.OptimizationStatus.Attempted = true
	optimized, err := s.computeClusterRequirements(ctx, params, workloads)
	if err != nil {
		result.OptimizationStatus.Reason = models.OptimizationCalculationError
		return
	}

	cpuPercent := optimizedCPU / totalCPU * 100.0
	memoryPercent := optimizedMemory / totalMemory * 100.0
	confidence := weightedConfidence / totalCPU
	optimized.ClusterSizing.CPUUtilizationMax = &cpuPercent
	optimized.ClusterSizing.MemoryUtilizationMax = &memoryPercent
	optimized.ClusterSizing.Confidence = &confidence
	result.OptimizedSizing = &optimized.ClusterSizing
	result.OptimizationStatus.Reason = models.OptimizationSuccess

	nodesSaved := result.ClusterSizing.TotalNodes - optimized.ClusterSizing.TotalNodes
	if nodesSaved > 0 {
		result.Savings = &models.Savings{
			NodesSaved:          nodesSaved,
			PercentageReduction: float64(nodesSaved) / float64(result.ClusterSizing.TotalNodes) * 100.0,
			Description:         "Based on actual workload performance data",
		}
	}
}
