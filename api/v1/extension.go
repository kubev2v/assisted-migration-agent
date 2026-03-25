package v1

import (
	"fmt"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func (a *AgentStatus) FromModel(m models.AgentStatus) {
	switch m.Console.Current {
	case models.ConsoleStatusConnected:
		a.ConsoleConnection = AgentStatusConsoleConnection("connected")
	case models.ConsoleStatusDisconnected:
		a.ConsoleConnection = AgentStatusConsoleConnection("disconnected")
	}
	if m.Console.Error != nil {
		err := m.Console.Error.Error()
		a.Error = &err
	}
	a.Mode = AgentStatusMode(m.Console.Target)
}

// NewVirtualMachineFromSummary converts a models.VirtualMachineSummary to an API VirtualMachine.
func NewVirtualMachineFromSummary(vm models.VirtualMachineSummary) VirtualMachine {
	result := VirtualMachine{
		Id:           vm.ID,
		Name:         vm.Name,
		Cluster:      vm.Cluster,
		Datacenter:   vm.Datacenter,
		DiskSize:     vm.DiskSize,
		Memory:       int64(vm.Memory),
		VCenterState: vm.PowerState,
		IssueCount:   vm.IssueCount,
		Migratable:   &vm.IsMigratable,
		Template:     &vm.IsTemplate,
		Inspection:   NewInspectionStatus(vm.Status),
	}
	if len(vm.Tags) > 0 {
		result.Tags = &vm.Tags
	}
	return result
}

func NewCollectorStatus(status models.CollectorStatus) CollectorStatus {
	var c CollectorStatus

	switch status.State {
	case models.CollectorStateReady:
		c.Status = CollectorStatusStatusReady
	case models.CollectorStateConnecting:
		c.Status = CollectorStatusStatusConnecting
	case models.CollectorStateCollecting:
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateCollected:
		c.Status = CollectorStatusStatusCollected
	case models.CollectorStateError:
		c.Status = CollectorStatusStatusError
	case models.CollectorStateParsing:
		c.Status = CollectorStatusStatusParsing
	default:
		c.Status = "unknown state"
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

func NewVirtualMachineDetailFromModel(vm models.VM) VirtualMachineDetail {
	details := VirtualMachineDetail{
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

	details.Template = &vm.IsTemplate
	details.Migratable = &vm.IsMigratable
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
		issues := make([]VMIssue, 0, len(vm.Issues))
		for _, issue := range vm.Issues {
			description := issue.Description
			if description == "" {
				description = issue.Label
			}
			vmIssue := VMIssue{
				Label:       issue.Label,
				Category:    VMIssueCategory(issue.Category),
				Description: description,
			}
			issues = append(issues, vmIssue)
		}
		details.Issues = &issues
	}

	return details
}

func NewInspectorStatus(status models.InspectorStatus) *InspectorStatus {
	var c InspectorStatus

	switch status.State {
	case models.InspectorStateReady:
		c.State = InspectorStatusStateReady
	case models.InspectorStateInitiating:
		c.State = InspectorStatusStateInitiating
	case models.InspectorStateRunning:
		c.State = InspectorStatusStateRunning
	case models.InspectorStateCanceled:
		c.State = InspectorStatusStateCanceled
	case models.InspectorStateCompleted:
		c.State = InspectorStatusStateCompleted
	case models.InspectorStateError:
		c.State = InspectorStatusStateError
	default:
		c.State = InspectorStatusStateReady
	}

	if status.Error != nil {
		e := status.Error.Error()
		c.Error = &e
	}

	return &c
}

func (s *InspectorStatus) WithCredentials(c *models.Credentials) *InspectorStatus {
	if c != nil {
		vc := VcenterCredentials{
			Url:      c.URL,
			Username: c.Username,
		}
		s.Credentials = &vc
	}
	return s
}

func (s *InspectorStatus) WithVddk(v *models.VddkStatus) *InspectorStatus {
	if v != nil {
		s.Vddk = &VddkProperties{
			Version: v.Version,
			Md5:     v.Md5,
		}
	}
	return s
}

func NewInspectionStatus(status models.InspectionStatus) VmInspectionStatus {
	var c VmInspectionStatus
	switch status.State.Value() {
	case models.InspectionStatePending.Value():
		c.State = VmInspectionStatusStatePending
	case models.InspectionStateRunning.Value():
		c.State = VmInspectionStatusStateRunning
	case models.InspectionStateCanceled.Value():
		c.State = VmInspectionStatusStateCanceled
	case models.InspectionStateCompleted.Value():
		c.State = VmInspectionStatusStateCompleted
	case models.InspectionStateError.Value():
		c.State = VmInspectionStatusStateError
	default:
		c.State = VmInspectionStatusStateNotStarted
	}

	if status.Error != nil {
		err := status.Error.Error()
		c.Error = &err
	}

	return c
}

// NewGroupFromModel converts a models.Group to an API Group.
func NewGroupFromModel(g models.Group) Group {
	id := fmt.Sprintf("%d", g.ID)
	createdAt := g.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	updatedAt := g.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	group := Group{
		Id:        id,
		Name:      g.Name,
		Filter:    g.Filter,
		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
	}
	if g.Description != "" {
		group.Description = &g.Description
	}
	if len(g.Tags) > 0 {
		group.Tags = &g.Tags
	}
	return group
}
