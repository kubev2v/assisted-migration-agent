package v1

import (
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func (a *AgentStatus) FromModel(m models.AgentStatus) {
	a.ConsoleConnection = AgentStatusConsoleConnection(m.Console.Current)
	a.Mode = AgentStatusMode(m.Console.Target)
}

// NewVMFromModel converts a models.VM to an API VM.
func NewVMFromModel(vm models.VM) VM {
	var inspectionState InspectionStatusState
	switch vm.InspectionState {
	case "completed":
		inspectionState = InspectionStatusStateCompleted
	case "running":
		inspectionState = InspectionStatusStateRunning
	case "error":
		inspectionState = InspectionStatusStateError
	default:
		inspectionState = InspectionStatusStatePending
	}

	apiVM := VM{
		Id:           vm.ID,
		Name:         vm.Name,
		Cluster:      vm.Cluster,
		Datacenter:   vm.Datacenter,
		DiskSize:     vm.DiskSize,
		Memory:       int64(vm.MemoryMB),
		VCenterState: vm.PowerState,
		Issues:       vm.Issues,
		Inspection: InspectionStatus{
			State: inspectionState,
		},
	}

	if vm.InspectionError != "" {
		apiVM.Inspection.Error = &vm.InspectionError
	}

	return apiVM
}

func NewCollectorStatus(status models.CollectorStatus) CollectorStatus {
	var c CollectorStatus

	switch status.State {
	case models.CollectorStateReady:
		c.Status = CollectorStatusStatusReady
	case models.CollectorStateConnecting:
		c.Status = CollectorStatusStatusConnecting
	case models.CollectorStateConnected:
		c.Status = CollectorStatusStatusConnected
	case models.CollectorStateCollecting:
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateCollected:
		c.Status = CollectorStatusStatusCollected
	case models.CollectorStateError:
		c.Status = CollectorStatusStatusError
	default:
		c.Status = CollectorStatusStatusReady
	}

	if status.Error != nil {
		e := status.Error.Error()
		c.Error = &e
	}

	return c
}

func NewCollectorStatusWithError(status models.CollectorStatus, err error) CollectorStatus {
	c := NewCollectorStatus(status)
	if err != nil {
		errStr := err.Error()
		c.Error = &errStr
	}
	return c
}

func NewVMDetailsFromModel(vm models.VM) VMDetails {
	details := VMDetails{
		Id:              vm.ID,
		Name:            vm.Name,
		PowerState:      vm.PowerState,
		ConnectionState: vm.ConnectionState,
		CpuCount:        vm.CpuCount,
		CoresPerSocket:  vm.CoresPerSocket,
		MemoryMB:        vm.MemoryMB,
		Disks:           make([]VMDisk, 0, len(vm.Disks)),
		Nics:            make([]VMNIC, 0, len(vm.NICs)),
	}

	if vm.UUID != "" {
		details.Uuid = &vm.UUID
	}
	if vm.Firmware != "" {
		details.Firmware = &vm.Firmware
	}
	if vm.Host != "" {
		details.Host = &vm.Host
	}
	if vm.Datacenter != "" {
		details.Datacenter = &vm.Datacenter
	}
	if vm.Cluster != "" {
		details.Cluster = &vm.Cluster
	}
	if vm.Folder != "" {
		details.Folder = &vm.Folder
	}
	if vm.GuestName != "" {
		details.GuestName = &vm.GuestName
	}
	if vm.GuestID != "" {
		details.GuestId = &vm.GuestID
	}
	if vm.HostName != "" {
		details.HostName = &vm.HostName
	}
	if vm.IPAddress != "" {
		details.IpAddress = &vm.IPAddress
	}
	if vm.StorageUsed > 0 {
		details.StorageUsed = &vm.StorageUsed
	}
	if vm.ToolsStatus != "" {
		details.ToolsStatus = &vm.ToolsStatus
	}
	if vm.ToolsRunningStatus != "" {
		details.ToolsRunningStatus = &vm.ToolsRunningStatus
	}

	details.IsTemplate = &vm.IsTemplate
	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled

	for _, d := range vm.Disks {
		// Convert MiB to bytes (parser returns capacity in MiB)
		capacityBytes := d.Capacity * 1024 * 1024
		disk := VMDisk{
			File:     &d.File,
			Capacity: &capacityBytes,
			Shared:   &d.Shared,
			Rdm:      &d.RDM,
			Bus:      &d.Bus,
			Mode:     &d.Mode,
		}
		if d.Key != 0 {
			key := d.Key
			disk.Key = &key
		}
		details.Disks = append(details.Disks, disk)
	}

	for _, n := range vm.NICs {
		nic := VMNIC{
			Mac:     &n.MAC,
			Network: &n.Network,
			Index:   &n.Index,
		}
		details.Nics = append(details.Nics, nic)
	}

	if len(vm.Issues) > 0 {
		details.Issues = &vm.Issues
	}

	return details
}
