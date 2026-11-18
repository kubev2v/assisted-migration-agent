package services

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/kubev2v/assisted-migration-agent/pkg/sizer"

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
	minSizingUtilizationConfidence = 50.0
)

type hostedControlPlaneCluster struct {
	form models.ClusterRequirementsRequestForm
}
type snoCluster struct {
	form models.ClusterRequirementsRequestForm
}
type haCluster struct {
	form        models.ClusterRequirementsRequestForm
	compactMode bool
}

type SizerService struct {
	store *store.Store
}

type sizingOptimizationMetrics struct {
	totalCPU           float64
	totalMemory        float64
	optimizedCPU       float64
	optimizedMemory    float64
	weightedConfidence float64
	hasUsableData      bool
	hasLowConfidence   bool
}

type Cluster interface {
	PreValidations() error
	Prepare(workloads []sizer.Workload) ([]sizer.Workload, []sizer.MachineSet)
	Compute(ctx context.Context, workloads []sizer.Workload, machineSets []sizer.MachineSet) (*sizer.SizerResponse, error)
	PostValidations(response *sizer.SizerResponse) error
}

func NewSizerService(st *store.Store) *SizerService {
	return &SizerService{store: st}
}

func (s *SizerService) CalculateClusterRequirements(ctx context.Context, form models.ClusterRequirementsRequestForm) (*models.ClusterRequirementsResponse, error) {
	form = form.WithDefaults()

	vms, total, err := NewVMService(s.store).List(ctx, VMListParams{Expression: form.Expression})
	if err != nil {
		return nil, fmt.Errorf("listing VMs for sizing: %w", err)
	}

	if total == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no VMs available for sizing for the provided expression: %s", form.Expression))
	}

	workloads := make([]sizer.Workload, 0, len(vms))
	var totals models.InventoryTotals
	var totalMemoryMiB int64

	for _, vm := range vms {
		if vm.MigrationExcluded {
			continue
		}

		if vm.CpuCount < 0 || vm.Memory < 0 {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("VM %q must have non negative CPU and memory resources", vm.ID))
		}

		workloads = append(workloads, newVMWorkload(vm, form))

		totals.TotalVMs++
		totals.TotalCPU += int(vm.CpuCount)
		totalMemoryMiB += int64(vm.Memory)
	}

	if totals.TotalVMs == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no VMs available for sizing for the provided expression: %s", form.Expression))
	}

	// Round once over the aggregate so sub-GB VMs do not each round up.
	totals.TotalMemory = int(math.Round(float64(totalMemoryMiB) / 1024.0))

	result, err := s.computeClusterRequirements(ctx, form, workloads)
	if err != nil {
		return nil, err
	}

	result.InventoryTotals = totals
	s.addOptimizedSizing(ctx, form, vms, result)

	return result, nil
}

func (s *SizerService) computeClusterRequirements(ctx context.Context, form models.ClusterRequirementsRequestForm, workloads []sizer.Workload) (*models.ClusterRequirementsResponse, error) {
	cluster := s.newCluster(form)

	if err := cluster.PreValidations(); err != nil {
		return nil, srvErrors.NewValidationError(err.Error())
	}

	workloads, machineSet := cluster.Prepare(workloads)

	response, err := cluster.Compute(ctx, workloads, machineSet)
	if err != nil {
		return nil, err
	}

	if err := cluster.PostValidations(response); err != nil {
		return nil, err
	}

	return buildResult(response), nil
}

func buildResult(response *sizer.SizerResponse) *models.ClusterRequirementsResponse {
	var workerNodes, controlPlaneNodes int
	for _, node := range response.Nodes {
		if node.IsControlPlane {
			controlPlaneNodes++
		} else {
			workerNodes++
		}
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
			TotalCPU:          response.TotalCPU,
			TotalMemory:       response.TotalMemory,
		},
		ResourceConsumption: models.SizingResourceConsumption{
			CPU:    response.ResourceConsumption.CPU,
			Memory: response.ResourceConsumption.Memory,
		},
	}

	if response.ResourceConsumption.Limits != nil {
		result.ResourceConsumption.Limits = &models.SizingResourceLimits{
			CPU:    response.ResourceConsumption.Limits.CPU,
			Memory: response.ResourceConsumption.Limits.Memory,
		}
	}

	if response.ResourceConsumption.OverCommitRatio != nil {
		result.ResourceConsumption.OverCommitRatio = &models.SizingOverCommitRatio{
			CPU:    response.ResourceConsumption.OverCommitRatio.CPU,
			Memory: response.ResourceConsumption.OverCommitRatio.Memory,
		}
	}

	return result
}

func (s *SizerService) addOptimizedSizing(
	ctx context.Context,
	form models.ClusterRequirementsRequestForm,
	vms []models.VirtualMachineSummary,
	result *models.ClusterRequirementsResponse,
) {
	result.OptimizationStatus = &models.OptimizationStatus{
		Reason: models.OptimizationNoUtilizationData,
	}

	workloads := make([]sizer.Workload, 0, len(vms))
	var metrics sizingOptimizationMetrics

	for _, vm := range vms {
		if vm.MigrationExcluded {
			continue
		}

		workload := newVMWorkload(vm, form)
		service := &workload.Services[0]

		metrics.recordVM(vm, service)
		workloads = append(workloads, workload)
	}

	if !metrics.hasUsableData {
		if metrics.hasLowConfidence {
			result.OptimizationStatus.Reason = models.OptimizationLowConfidence
		}
		return
	}

	if metrics.totalCPU == 0 || metrics.totalMemory == 0 {
		return
	}

	result.OptimizationStatus.Attempted = true
	optimized, err := s.computeClusterRequirements(ctx, form, workloads)
	if err != nil {
		result.OptimizationStatus.Reason = models.OptimizationCalculationError
		return
	}

	cpuPercent := metrics.optimizedCPU / metrics.totalCPU * 100.0
	memoryPercent := metrics.optimizedMemory / metrics.totalMemory * 100.0
	confidence := metrics.weightedConfidence / metrics.totalCPU
	optimized.ClusterSizing.CPUUtilizationMax = &cpuPercent
	optimized.ClusterSizing.MemoryUtilizationMax = &memoryPercent
	optimized.ClusterSizing.Confidence = &confidence

	result.OptimizedSizing = &optimized.ClusterSizing
	result.OptimizationStatus.Reason = models.OptimizationSuccess

	baselineNodes := result.ClusterSizing.TotalNodes
	optimizedNodes := optimized.ClusterSizing.TotalNodes
	saved := baselineNodes - optimizedNodes
	if saved > 0 {
		result.Savings = &models.Savings{
			NodesSaved:          saved,
			PercentageReduction: float64(saved) / float64(baselineNodes) * 100.0,
			Description:         "Based on actual workload performance data",
		}
	}
}

func (m *sizingOptimizationMetrics) recordVM(vm models.VirtualMachineSummary, service *sizer.Service) {
	m.totalCPU += service.LimitCPU
	m.totalMemory += service.LimitMemory

	confidence, hasConfidence := validPositiveFloat(vm.UtilizationConfidence)
	if hasConfidence {
		m.weightedConfidence += service.LimitCPU * confidence
	}

	if hasConfidence && confidence < minSizingUtilizationConfidence {
		m.hasLowConfidence = true
	}

	if confidence >= minSizingUtilizationConfidence {
		if cpuPct, memPct, ok := validUtilization(vm); ok {
			cpuMultiplier := math.Min(cpuPct/100.0, 1.0)
			memMultiplier := math.Min(memPct/100.0, 1.0)
			service.RequiredCPU *= cpuMultiplier
			service.RequiredMemory *= memMultiplier
			service.LimitCPU *= cpuMultiplier
			service.LimitMemory *= memMultiplier
			m.hasUsableData = true
		}
	}

	m.optimizedCPU += service.LimitCPU
	m.optimizedMemory += service.LimitMemory
}

func (s *SizerService) newCluster(form models.ClusterRequirementsRequestForm) Cluster {
	if form.HostedControlPlane != nil && *form.HostedControlPlane {
		return &hostedControlPlaneCluster{
			form: form,
		}
	}
	if form.ControlPlaneNodeCount != nil && *form.ControlPlaneNodeCount == 1 {
		return &snoCluster{
			form: form,
		}
	}

	cluster := &haCluster{
		form: form,
	}

	if *form.CompactMode {
		cluster.compactMode = true
	}

	return cluster
}

func newVMWorkload(vm models.VirtualMachineSummary, form models.ClusterRequirementsRequestForm) sizer.Workload {
	cpu := float64(vm.CpuCount)
	memoryGB := float64(vm.Memory) / 1024.0

	return sizer.Workload{
		Name:  vm.ID,
		Count: 1,
		Services: []sizer.Service{{
			Name:           vm.ID,
			RequiredCPU:    cpu / form.CPUOverCommitMultiplier,
			RequiredMemory: memoryGB / form.MemoryOverCommitMultiplier,
			LimitCPU:       cpu,
			LimitMemory:    memoryGB,
		}},
	}
}

func newWorkerMachineSet(form models.ClusterRequirementsRequestForm) sizer.MachineSet {
	effectiveCPU := float64(form.WorkerNodeCPU)
	effectiveCPU += float64(*form.WorkerNodeThreads-form.WorkerNodeCPU) * smtThreadCapacity

	return sizer.MachineSet{
		Name:          workerMachineSetName,
		CPU:           int(math.Round(effectiveCPU)),
		Memory:        form.WorkerNodeMemory,
		InstanceName:  "bare-metal-worker",
		NumberOfDisks: machineSetNumberOfDisks,
		Label:         "Worker",
	}
}

func newControlPlaneMachineSet(form models.ClusterRequirementsRequestForm) sizer.MachineSet {
	allowControlPlaneWorkloads := true
	return sizer.MachineSet{
		Name:                    controlPlaneMachineSetName,
		IsControlPlane:          true,
		CPU:                     *form.ControlPlaneCPU,
		Memory:                  *form.ControlPlaneMemory,
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

func compute(ctx context.Context, workloads []sizer.Workload, machineSets []sizer.MachineSet, errPrefix string) (*sizer.SizerResponse, error) {
	sizing, err := sizer.NewSizer().Calculate(ctx, machineSets, workloads, true)
	if err != nil {
		var resourceErr *sizer.NotEnoughResourcesError
		if errors.As(err, &resourceErr) {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("%s: %v", errPrefix, err))
		}
		return nil, fmt.Errorf("calculating cluster requirements: %w", err)
	}

	return sizing, nil
}

// preValidations holds the field-level checks that apply to every cluster
// topology. Each Cluster implementation calls it first from its own PreValidations,
// then layers the rules specific to that topology.
func preValidations(form models.ClusterRequirementsRequestForm) error {
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

	if form.ControlPlaneCPU != nil && (*form.ControlPlaneCPU < 2 || *form.ControlPlaneCPU > 384) {
		return errors.New("controlPlaneCPU must be between 2 and 384")
	}

	if form.ControlPlaneMemory != nil && (*form.ControlPlaneMemory < 4 || *form.ControlPlaneMemory > 4096) {
		return errors.New("controlPlaneMemory must be between 4 and 4096 GB")
	}

	if form.ControlPlaneNodeCount != nil && *form.ControlPlaneNodeCount != 1 && *form.ControlPlaneNodeCount != 3 {
		return errors.New("controlPlaneNodeCount must be 1 or 3")
	}

	return nil
}

func postValidations(response *sizer.SizerResponse) error {
	if response.NodeCount > maxNodeCount {
		return fmt.Errorf("cluster requires %d nodes, exceeding the maximum of %d; use larger nodes", response.NodeCount, maxNodeCount)
	}

	return nil
}

func validUtilization(vm models.VirtualMachineSummary) (cpuPct, memPct float64, ok bool) {
	cpuPct, cpuOK := validPositiveFloat(vm.UtilizationCpuMax)
	memPct, memOK := validPositiveFloat(vm.UtilizationMemMax)
	return cpuPct, memPct, cpuOK && memOK
}

func validPositiveFloat(val *float64) (float64, bool) {
	if val == nil || math.IsNaN(*val) || math.IsInf(*val, 0) || *val <= 0 {
		return 0, false
	}
	return *val, true
}

func (h *hostedControlPlaneCluster) PreValidations() error {
	if err := preValidations(h.form); err != nil {
		return err
	}

	if h.form.ControlPlaneNodeCount != nil {
		return errors.New("controlPlaneNodeCount cannot be specified when hostedControlPlane is true")
	}

	if h.form.ControlPlaneCPU != nil {
		return errors.New("controlPlaneCPU cannot be specified when hostedControlPlane is true")
	}

	if h.form.ControlPlaneMemory != nil {
		return errors.New("controlPlaneMemory cannot be specified when hostedControlPlane is true")
	}

	if h.form.ControlPlaneSchedulable != nil {
		return errors.New("controlPlaneSchedulable cannot be specified when hostedControlPlane is true")
	}

	if h.form.CompactMode != nil && *h.form.CompactMode {
		return errors.New("compactMode cannot be true when hostedControlPlane is true")
	}

	return nil
}

func (h *hostedControlPlaneCluster) Prepare(workloads []sizer.Workload) ([]sizer.Workload, []sizer.MachineSet) {
	machineSets := []sizer.MachineSet{newWorkerMachineSet(h.form)}
	for i := range workloads {
		workloads[i].AllowedMachineSets = []string{workerMachineSetName}
	}

	return workloads, machineSets
}

func (h *hostedControlPlaneCluster) Compute(ctx context.Context, workloads []sizer.Workload, machineSets []sizer.MachineSet) (*sizer.SizerResponse, error) {
	return compute(ctx, workloads, machineSets, "configured nodes cannot fit a workload")
}

func (h *hostedControlPlaneCluster) PostValidations(response *sizer.SizerResponse) error {
	return postValidations(response)
}

func (s *snoCluster) PreValidations() error {
	if err := preValidations(s.form); err != nil {
		return err
	}

	if s.form.ControlPlaneSchedulable == nil || !*s.form.ControlPlaneSchedulable {
		return errors.New("single-node clusters require controlPlaneSchedulable to be true")
	}

	return nil
}

func (s *snoCluster) Prepare(workloads []sizer.Workload) ([]sizer.Workload, []sizer.MachineSet) {
	machineSets := []sizer.MachineSet{newControlPlaneMachineSet(s.form)}
	for i := range workloads {
		workloads[i].AllowedMachineSets = []string{controlPlaneMachineSetName}
		// Binding every VM to the single control-plane service prevents extra nodes from being created.
		workloads[i].Services[0].RunsWith = []string{controlPlaneServiceName}
	}
	workloads = append([]sizer.Workload{newControlPlaneWorkload(*s.form.ControlPlaneNodeCount)}, workloads...)

	return workloads, machineSets
}

func (s *snoCluster) Compute(ctx context.Context, workloads []sizer.Workload, machineSets []sizer.MachineSet) (*sizer.SizerResponse, error) {
	return compute(ctx, workloads, machineSets, "workload does not fit on a single control-plane node")
}

func (s *snoCluster) PostValidations(response *sizer.SizerResponse) error {
	return postValidations(response)
}

func (h *haCluster) PreValidations() error {
	if err := preValidations(h.form); err != nil {
		return err
	}

	if h.compactMode {
		if h.form.ControlPlaneNodeCount == nil || *h.form.ControlPlaneNodeCount != 3 {
			return errors.New("compactMode requires controlPlaneNodeCount to be 3")
		}

		if h.form.ControlPlaneSchedulable == nil || !*h.form.ControlPlaneSchedulable {
			return errors.New("compactMode requires controlPlaneSchedulable to be true")
		}
	}

	return nil
}

func (h *haCluster) Prepare(workloads []sizer.Workload) ([]sizer.Workload, []sizer.MachineSet) {
	machineSets := make([]sizer.MachineSet, 0, 2)
	vmMachineSets := make([]string, 0, 2)

	if !h.compactMode {
		machineSets = append(machineSets, newWorkerMachineSet(h.form))
		vmMachineSets = append(vmMachineSets, workerMachineSetName)
	}

	machineSets = append(machineSets, newControlPlaneMachineSet(h.form))
	if *h.form.ControlPlaneSchedulable {
		vmMachineSets = append(vmMachineSets, controlPlaneMachineSetName)
	}

	for i := range workloads {
		workloads[i].AllowedMachineSets = vmMachineSets
	}

	workloads = append([]sizer.Workload{newControlPlaneWorkload(*h.form.ControlPlaneNodeCount)}, workloads...)

	return workloads, machineSets
}

func (h *haCluster) Compute(ctx context.Context, workloads []sizer.Workload, machineSets []sizer.MachineSet) (*sizer.SizerResponse, error) {
	prefix := "configured nodes cannot fit a workload"
	if h.compactMode {
		prefix = "workload does not fit in compact mode"
	}

	return compute(ctx, workloads, machineSets, prefix)
}

func (h *haCluster) PostValidations(response *sizer.SizerResponse) error {
	if err := postValidations(response); err != nil {
		return err
	}

	if h.compactMode {
		if response.NodeCount > *h.form.ControlPlaneNodeCount {
			return fmt.Errorf("workload does not fit in compact mode: requires %d nodes, maximum is %d", response.NodeCount, *h.form.ControlPlaneNodeCount)
		}
		return nil
	}

	controlPlaneNodes := 0
	for _, node := range response.Nodes {
		if node.IsControlPlane {
			controlPlaneNodes++
		}
	}

	if controlPlaneNodes > *h.form.ControlPlaneNodeCount {
		return fmt.Errorf("workload requires %d control-plane nodes, but %d are configured; use larger worker or control-plane nodes", controlPlaneNodes, *h.form.ControlPlaneNodeCount)
	}

	return nil
}
