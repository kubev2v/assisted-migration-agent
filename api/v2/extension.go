package v2

import (
	"crypto/x509"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// CredsFromAPI converts a VcenterCredentials API type to models.Credentials.
func CredsFromAPI(v VcenterCredentials) (models.Credentials, error) {
	c := models.Credentials{
		URL:      v.Url,
		Username: v.Username,
		Password: v.Password,
	}
	if v.Cacert != nil {
		if v.SkipTls != nil && *v.SkipTls {
			return models.Credentials{}, errors.New("skipTls and cacert are mutually exclusive")
		}
		pemBytes := []byte(*v.Cacert)
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return models.Credentials{}, errors.New("cacert: no valid PEM certificates found")
		}
		c.CACert = pemBytes
	} else {
		// no cacert: default to skip-verify for backwards compat unless explicitly false
		c.SkipTLS = v.SkipTls == nil || *v.SkipTls
	}
	return c, nil
}

func (a *AgentStatus) FromModel(m models.AgentStatus) {
	switch m.Console.Current {
	case models.ConsoleStatusConnected:
		a.ConsoleConnection.Status = ConsoleConnectionStatusConnected
	case models.ConsoleStatusDisconnected:
		a.ConsoleConnection.Status = ConsoleConnectionStatusDisconnected
	}
	if m.Console.Error != nil {
		err := m.Console.Error.Error()
		a.ConsoleConnection.Error = &err
	}
	a.Mode = AgentStatusMode(m.Console.Target)
}

// NewVirtualMachineFromSummary converts a models.VirtualMachineSummary to a v2 VirtualMachine.
func NewVirtualMachineFromSummary(vm models.VirtualMachineSummary) VirtualMachine {
	result := VirtualMachine{
		Id:           vm.ID,
		Name:         vm.Name,
		VCenterID:    vm.VCenterID,
		VCenterState: vm.PowerState,
		Cluster:      vm.Cluster,
		Datacenter:   vm.Datacenter,
		DiskSize:     vm.DiskSize,
		Memory:       int64(vm.Memory),
		IssueCount:   vm.IssueCount,
		Migratable:   &vm.IsMigratable,
		Template:     &vm.IsTemplate,
	}

	if vm.InspectionStatus.State != models.InspectionStateNotStarted {
		s := NewInspectionStatus(vm.InspectionStatus)
		result.InspectionStatus = &s
	}
	if vm.InspectionConcernCount > 0 {
		result.InspectionConcernCount = &vm.InspectionConcernCount
	}
	if len(vm.Tags) > 0 {
		result.Tags = &vm.Tags
	}

	return result
}

// NewVirtualMachineDetailFromModel converts a models.VM to a v2 VirtualMachineDetail.
func NewVirtualMachineDetailFromModel(vm models.VM) VirtualMachineDetail {
	details := VirtualMachineDetail{
		Id:              vm.ID,
		Name:            vm.Name,
		VCenterID:       vm.VCenterID,
		PowerState:      vm.PowerState,
		ConnectionState: vm.ConnectionState,
		CpuCount:        vm.CpuCount,
		CoresPerSocket:  vm.CoresPerSocket,
		MemoryMB:        vm.MemoryMB,
		Disks:           make([]VirtualMachineDisk, 0, len(vm.Disks)),
		Nics:            make([]VirtualMachineNIC, 0, len(vm.NICs)),
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
	if len(vm.CpuAffinity) > 0 {
		details.CpuAffinity = &vm.CpuAffinity
	}

	details.Template = &vm.IsTemplate
	details.Migratable = &vm.IsMigratable
	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled

	for _, d := range vm.Disks {
		capacityBytes := d.Capacity * 1024 * 1024
		disk := VirtualMachineDisk{
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
		nic := VirtualMachineNIC{
			Mac:     &n.MAC,
			Network: &n.Network,
			Index:   &n.Index,
		}
		details.Nics = append(details.Nics, nic)
	}

	if len(vm.Devices) > 0 {
		devices := make([]VirtualMachineDevice, 0, len(vm.Devices))
		for _, d := range vm.Devices {
			devices = append(devices, VirtualMachineDevice{Kind: &d.Kind})
		}
		details.Devices = &devices
	}

	if len(vm.GuestNetworks) > 0 {
		networks := make([]GuestNetwork, 0, len(vm.GuestNetworks))
		for _, g := range vm.GuestNetworks {
			gn := GuestNetwork{
				Mac: &g.MAC,
				Ip:  &g.IP,
			}
			if g.Device != "" {
				gn.Device = &g.Device
			}
			if g.Network != "" {
				gn.Network = &g.Network
			}
			if g.PrefixLength > 0 {
				gn.PrefixLength = &g.PrefixLength
			}
			networks = append(networks, gn)
		}
		details.GuestNetworks = &networks
	}

	if len(vm.InspectionConcerns) > 0 {
		concerns := make([]VirtualMachineInspectionConcern, 0, len(vm.InspectionConcerns))
		for _, co := range vm.InspectionConcerns {
			concerns = append(concerns, VirtualMachineInspectionConcern{
				Category: co.Category,
				Label:    co.Label,
				Message:  co.Msg,
			})
		}
		details.Inspection = &VirtualMachineInspectionResults{Concerns: &concerns}
	}

	if len(vm.Issues) > 0 {
		issues := make([]VirtualMachineIssue, 0, len(vm.Issues))
		for _, issue := range vm.Issues {
			description := issue.Description
			if description == "" {
				description = issue.Label
			}
			issues = append(issues, VirtualMachineIssue{
				Label:       issue.Label,
				Category:    VirtualMachineIssueCategory(issue.Category),
				Description: description,
			})
		}
		details.Issues = &issues
	}

	return details
}

func NewCollectionFromDatabase(db *store.Database) Collection {
	name := strings.TrimSuffix(db.Path, filepath.Ext(db.Path))
	return Collection{
		Id:        db.ID,
		Name:      name,
		CreatedAt: db.CreatedAt,
	}
}

// NewCollectorStatus converts a models.CollectorStatus to a v2 CollectorStatus.
func NewCollectorStatus(status models.CollectorStatus) CollectorStatus {
	var c CollectorStatus
	c.Id = status.ID

	switch status.State {
	case models.CollectorStateConnecting:
		c.Status = CollectorStatusStatusConnecting
	// TODO: fix rightsizing status
	case models.CollectorStateCollecting, models.CollectorStateRightsizingConnecting:
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateParsing:
		c.Status = CollectorStatusStatusParsing
	case models.CollectorStateCollected:
		c.Status = CollectorStatusStatusCollected
	case models.CollectorStateError:
		c.Status = CollectorStatusStatusError
	default:
		c.Status = CollectorStatusStatusConnecting
	}

	if status.Error != nil {
		e := status.Error.Error()
		c.Error = &e
	}

	return c
}

// NewInspectionStatus converts a models.InspectionStatus to a v2 InspectionStatus.
func NewInspectionStatus(status models.InspectionStatus) InspectionStatus {
	var s InspectionStatus
	switch status.State.Value() {
	case models.InspectionStatePending.Value():
		s.State = InspectionStatusStatePending
	case models.InspectionStateRunning.Value():
		s.State = InspectionStatusStateRunning
	case models.InspectionStateCompleted.Value():
		s.State = InspectionStatusStateCompleted
	case models.InspectionStateCanceled.Value():
		s.State = InspectionStatusStateCanceled
	case models.InspectionStateError.Value():
		s.State = InspectionStatusStateError
	}

	if status.Error != nil {
		err := status.Error.Error()
		s.Error = &err
	}

	return s
}

// NewRightsizingClusterUtilizationFromModel converts a models.RightsizingClusterUtilization to the API type.
func NewRightsizingClusterUtilizationFromModel(c models.RightsizingClusterUtilization) RightsizingClusterUtilization {
	return RightsizingClusterUtilization{
		ClusterId:                c.ClusterID,
		ClusterName:              c.ClusterName,
		VmCount:                  c.VMCount,
		CpuAvg:                   c.CpuAvg,
		CpuP95:                   c.CpuP95,
		CpuMax:                   c.CpuMax,
		MemAvg:                   c.MemAvg,
		MemP95:                   c.MemP95,
		MemMax:                   c.MemMax,
		Disk:                     c.Disk,
		Confidence:               c.Confidence,
		TotalProvisionedCpus:     int(c.TotalProvisionedCpus),
		TotalProvisionedMemoryMb: int(c.TotalProvisionedMemoryMb),
		TotalProvisionedDiskKb:   c.TotalProvisionedDiskKb,
	}
}

// NewVmUtilizationDetailsFromModel converts a models.VmUtilizationDetails to the API type.
func NewVmUtilizationDetailsFromModel(d models.VmUtilizationDetails) VmUtilizationDetails {
	return VmUtilizationDetails{
		Moid:                d.MOID,
		VmName:              d.VMName,
		ProvisionedCpus:     d.ProvisionedCpus,
		ProvisionedMemoryMb: d.ProvisionedMemoryMb,
		ProvisionedDiskKb:   d.ProvisionedDiskKb,
		CpuAvg:              d.CpuAvg,
		CpuP95:              d.CpuP95,
		CpuMax:              d.CpuMax,
		CpuLatest:           d.CpuLatest,
		MemAvg:              d.MemAvg,
		MemP95:              d.MemP95,
		MemMax:              d.MemMax,
		MemLatest:           d.MemLatest,
		Disk:                d.Disk,
		Confidence:          d.Confidence,
	}
}

// NewGroupFromModel converts a models.Group to a v2 Group.
func NewGroupFromModel(g models.Group) Group {
	createdAt := g.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	updatedAt := g.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	group := Group{
		Id:        g.ID.String(),
		Name:      g.Name,
		Filter:    g.Filter,
		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
	}
	if g.Description != "" {
		group.Description = &g.Description
	}
	return group
}
