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
	workerMachineSetName       = "worker"
	controlPlaneMachineSetName = "controlPlane"
	controlPlaneServiceName    = "ControlPlane"
	machineSetNumberOfDisks    = 24
	controlPlaneReservedCPU    = 3.5
	controlPlaneReservedMemory = 13.39
	smtThreadCapacity          = 0.5
	minFailoverNodes           = 2
	failoverCapacityPercent    = 10.0
	maxNodeCount               = 100
	maxVMsPerWorkerNode        = 200
)

type SizerService struct {
	store *store.Store
}

func NewSizerService(st *store.Store) *SizerService {
	return &SizerService{store: st}
}

func (s *SizerService) CalculateClusterRequirements(ctx context.Context, form models.ClusterRequirementsRequestForm) (*models.ClusterRequirementsResponse, error) {
	err := form.Validate()
	if err != nil {
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

	hostedControlPlane := *params.HostedControlPlane
	controlPlaneNodeCount := 0
	if !hostedControlPlane {
		controlPlaneNodeCount = *params.ControlPlaneNodeCount
	}

	singleNode := controlPlaneNodeCount == 1
	compactMode := *params.CompactMode
	machineSets := make([]sizer.MachineSet, 0, 2)
	vmMachineSets := make([]string, 0, 2)

	if !singleNode && !compactMode {
		effectiveCPU := float64(params.WorkerNodeCPU)
		effectiveCPU += float64(*params.WorkerNodeThreads-params.WorkerNodeCPU) * smtThreadCapacity

		machineSets = append(machineSets, sizer.MachineSet{
			Name:          workerMachineSetName,
			CPU:           int(math.Round(effectiveCPU)),
			Memory:        params.WorkerNodeMemory,
			InstanceName:  "bare-metal-worker",
			NumberOfDisks: machineSetNumberOfDisks,
			Label:         "Worker",
		})
		vmMachineSets = append(vmMachineSets, workerMachineSetName)
	}

	workloads := make([]sizer.Workload, 0, len(vms)+1)
	if !hostedControlPlane {
		// The control-plane service must be schedulable; VM eligibility is restricted separately.
		allowControlPlaneWorkloads := true
		machineSets = append(machineSets, sizer.MachineSet{
			Name:                    controlPlaneMachineSetName,
			IsControlPlane:          true,
			CPU:                     *params.ControlPlaneCPU,
			Memory:                  *params.ControlPlaneMemory,
			InstanceName:            "control-plane",
			NumberOfDisks:           machineSetNumberOfDisks,
			Label:                   "Control Plane",
			AllowWorkloadScheduling: &allowControlPlaneWorkloads,
		})

		if *params.ControlPlaneSchedulable {
			vmMachineSets = append(vmMachineSets, controlPlaneMachineSetName)
		}

		// Reserve these resources through the workload, rather than also reserving them on the machine set.
		workloads = append(workloads, sizer.Workload{
			Name:               "control-plane-services",
			Count:              controlPlaneNodeCount,
			AllowedMachineSets: []string{controlPlaneMachineSetName},
			Services: []sizer.Service{{
				Name:           controlPlaneServiceName,
				RequiredCPU:    controlPlaneReservedCPU,
				RequiredMemory: controlPlaneReservedMemory,
			}},
		})
	}

	var vmRunsWith []string
	if singleNode {
		// Binding every VM to the single control-plane service prevents extra nodes from being created.
		vmRunsWith = []string{controlPlaneServiceName}
	}

	vmWorkloadOffset := len(workloads)
	var totals models.InventoryTotals

	for _, vm := range vms {
		if vm.MigrationExcluded {
			continue
		}

		if vm.CpuCount <= 0 || vm.Memory <= 0 {
			return nil, srvErrors.NewValidationError(fmt.Sprintf("VM %q must have positive CPU and memory resources", vm.ID))
		}

		cpu := float64(vm.CpuCount)
		memoryGB := float64(vm.Memory) / 1024.0
		name := vm.ID

		workloads = append(workloads, sizer.Workload{
			Name:               name,
			Count:              1,
			AllowedMachineSets: vmMachineSets,
			Services: []sizer.Service{{
				Name:           name,
				RequiredCPU:    cpu / params.CPUOverCommitMultiplier,
				RequiredMemory: memoryGB / params.MemoryOverCommitMultiplier,
				LimitCPU:       cpu,
				LimitMemory:    memoryGB,
				RunsWith:       vmRunsWith,
			}},
		})

		totals.TotalVMs++
		totals.TotalCPU += int(vm.CpuCount)
		// The planner rounds each VM's memory to GB before summing inventory totals.
		totals.TotalMemory += int(math.Round(memoryGB))
	}

	if totals.TotalVMs == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no VMs available for sizing for the provided expression: %s", params.Expression))
	}

	if !singleNode {
		// Keeping groups apart bounds the number of VM services placed on any one node.
		vmWorkloads := workloads[vmWorkloadOffset:]
		for i := range vmWorkloads {
			group := i / maxVMsPerWorkerNode
			for j := range vmWorkloads {
				if j/maxVMsPerWorkerNode != group {
					vmWorkloads[i].Services[0].Avoid = append(vmWorkloads[i].Services[0].Avoid, vmWorkloads[j].Services[0].Name)
				}
			}
		}
	}

	sizing, err := sizer.NewSizer().Calculate(ctx, machineSets, workloads, true)
	if err != nil {
		var resourceErr *sizer.NotEnoughResourcesError
		if errors.As(err, &resourceErr) {
			if singleNode {
				return nil, srvErrors.NewValidationError(fmt.Sprintf("workload does not fit on a single control-plane node: %v", err))
			}

			if compactMode {
				return nil, srvErrors.NewValidationError(fmt.Sprintf("workload does not fit in compact mode: %v", err))
			}

			return nil, srvErrors.NewValidationError(fmt.Sprintf("configured nodes cannot fit a workload: %v", err))
		}

		return nil, fmt.Errorf("calculating baseline cluster requirements: %w", err)
	}

	if singleNode && sizing.NodeCount > controlPlaneNodeCount {
		return nil, srvErrors.NewValidationError("workload does not fit on a single control-plane node")
	}

	if compactMode && sizing.NodeCount > controlPlaneNodeCount {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("workload does not fit in compact mode: requires %d nodes, maximum is %d", sizing.NodeCount, controlPlaneNodeCount))
	}

	if !singleNode && sizing.NodeCount > maxNodeCount {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("cluster requires %d nodes, exceeding the maximum of %d; use larger nodes", sizing.NodeCount, maxNodeCount))
	}

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

	// Match the planner: failover changes node counts while resource totals describe the calculated placement.
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
		InventoryTotals: totals,
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
