package v1

import (
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func (a *AgentStatus) FromModel(m models.AgentStatus) {
	a.ConsoleConnection = AgentStatusConsoleConnection(m.Console.Current)
	a.Mode = AgentStatusMode(m.Console.Target)
}

// NewVMFromSummary converts a models.VMSummary to an API VM.
func NewVMFromSummary(vm models.VMSummary) VM {
	return VM{
		Id:           vm.ID,
		Name:         vm.Name,
		Cluster:      vm.Cluster,
		DiskSize:     vm.DiskSize,
		Memory:       int64(vm.Memory),
		VCenterState: vm.PowerState,
		IssueCount:   vm.IssueCount,
		Inspection: InspectionStatus{
			State: InspectionStatusStatePending,
		},
	}
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

func NewInspectorStatus(status models.InspectorStatus) InspectorStatus {
	var c InspectorStatus

	switch status.State {
	case models.InspectorStateReady:
		c.State = InspectorStatusStateReady
	case models.InspectorStateRunning, models.InspectorStateConnecting:
		c.State = InspectorStatusStateRunning
	case models.InspectorStateCancelled:
		c.State = InspectorStatusStateCanceled
	case models.InspectorStateDone:
		c.State = InspectorStatusStateDone
	case models.InspectorStateError:
		c.State = InspectorStatusStateError
	default:
		c.State = InspectorStatusStateReady
	}

	if status.Error != nil {
		e := status.Error.Error()
		c.Error = &e
	}

	return c
}

func NewInspectionStatus(state string, err string) InspectionStatus {
	var c InspectionStatus
	switch state {
	case models.InspectionStatePending.Value():
		c.State = InspectionStatusStatePending
	case models.InspectionStateRunning.Value():
		c.State = InspectionStatusStateRunning
	case models.InspectionStateCanceled.Value():
		c.State = InspectionStatusStateCanceled
	case models.InspectionStateCompleted.Value():
		c.State = InspectionStatusStateCompleted
	case models.InspectionStateError.Value():
		c.State = InspectionStatusStateError
	default:
		c.State = InspectionStatusStateNotFound
	}

	if err != "" {
		c.Error = &err
	}

	return c
}

func FlatStatus(s models.InspectionStatus) (string, string) {
	var errStr string

	if s.Error != nil {
		errStr = s.Error.Error()
	}

	return s.State.Value(), errStr
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
