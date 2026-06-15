package v1

import (
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
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
		Id:                vm.ID,
		Name:              vm.Name,
		Cluster:           vm.Cluster,
		Datacenter:        vm.Datacenter,
		DiskSize:          vm.DiskSize,
		Memory:            int64(vm.Memory),
		VCenterState:      vm.PowerState,
		IssueCount:        vm.IssueCount,
		Migratable:        &vm.IsMigratable,
		Template:          &vm.IsTemplate,
		MigrationExcluded: &vm.MigrationExcluded,
	}
	if len(vm.Groups) > 0 {
		result.Groups = &vm.Groups
	}
	if len(vm.Labels) > 0 {
		result.Labels = &vm.Labels
	}

	if vm.InspectionStatus.State != models.InspectionStateNotStarted {
		s := NewInspectionStatus(vm.InspectionStatus)
		result.InspectionStatus = &s
	}
	if vm.InspectionConcernCount > 0 {
		result.InspectionConcernCount = &vm.InspectionConcernCount
	}

	result.UtilizationCpuP95 = vm.UtilizationCpuP95
	result.UtilizationMemP95 = vm.UtilizationMemP95
	result.UtilizationCpuMax = vm.UtilizationCpuMax
	result.UtilizationMemMax = vm.UtilizationMemMax
	result.UtilizationDisk = vm.UtilizationDisk
	result.UtilizationConfidence = vm.UtilizationConfidence

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
		c.Status = CollectorStatusStatusParsing
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
	if len(vm.InspectionConcerns) > 0 {
		concerns := make([]VmInspectionConcern, 0, len(vm.InspectionConcerns))
		for _, co := range vm.InspectionConcerns {
			concerns = append(concerns, VmInspectionConcern{
				Category: co.Category,
				Label:    co.Label,
				Message:  co.Msg,
			})
		}
		details.Inspection = &VmInspectionResults{Concerns: &concerns}
	}

	details.Template = &vm.IsTemplate
	details.Migratable = &vm.IsMigratable
	details.MigrationExcluded = &vm.MigrationExcluded
	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled
	if len(vm.Labels) > 0 {
		details.Labels = &vm.Labels
	}

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
		if n.IPv4Address != "" {
			nic.Ipv4Address = &n.IPv4Address
		}
		if n.IPv6Address != "" {
			nic.Ipv6Address = &n.IPv6Address
		}
		details.Nics = append(details.Nics, nic)
	}

	if vm.Utilization != nil {
		u := NewVmUtilizationDetailsFromModel(*vm.Utilization)
		details.Utilization = &u
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
	case models.InspectorStateRunning:
		c.State = InspectorStatusStateRunning
	default:
		c.State = InspectorStatusStateReady
	}

	return &c
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
	}

	c.Message = status.Details

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
	return group
}

// NewRightsizingMetricStatsFromModel converts a models.RightsizingMetricStats to the API type.
func NewRightsizingMetricStatsFromModel(s models.RightsizingMetricStats) RightsizingMetricStats {
	return RightsizingMetricStats{
		SampleCount: s.SampleCount,
		Average:     s.Average,
		P95:         s.P95,
		P99:         s.P99,
		Max:         s.Max,
		Latest:      s.Latest,
	}
}

// NewRightsizingVMReportFromModel converts a models.RightsizingVMReport to the API type.
func NewRightsizingVMReportFromModel(vm models.RightsizingVMReport) RightsizingVMReport {
	metrics := make(map[string]RightsizingMetricStats, len(vm.Metrics))
	for k, v := range vm.Metrics {
		metrics[k] = NewRightsizingMetricStatsFromModel(v)
	}
	warnings := vm.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return RightsizingVMReport{
		Name:     vm.Name,
		Moid:     vm.MOID,
		Metrics:  metrics,
		Warnings: warnings,
	}
}

// NewRightsizingReportSummaryFromModel converts a models.RightsizingReportSummary to the API type.
func NewRightsizingReportSummaryFromModel(r models.RightsizingReportSummary) RightsizingReportSummary {
	return RightsizingReportSummary{
		Id:                  r.ID,
		Vcenter:             r.VCenter,
		ClusterId:           r.ClusterID,
		WindowStart:         r.WindowStart,
		WindowEnd:           r.WindowEnd,
		IntervalId:          r.IntervalID,
		ExpectedSampleCount: r.ExpectedSampleCount,
		CreatedAt:           r.CreatedAt,
	}
}

// NewRightsizingReportFromModel converts a models.RightsizingReport to the API type.
func NewRightsizingReportFromModel(r models.RightsizingReport) RightsizingReport {
	vms := make([]RightsizingVMReport, 0, len(r.VMs))
	for _, vm := range r.VMs {
		vms = append(vms, NewRightsizingVMReportFromModel(vm))
	}
	return RightsizingReport{
		Id:                  r.ID,
		Vcenter:             r.VCenter,
		ClusterId:           r.ClusterID,
		WindowStart:         r.WindowStart,
		WindowEnd:           r.WindowEnd,
		IntervalId:          r.IntervalID,
		ExpectedSampleCount: r.ExpectedSampleCount,
		Vms:                 vms,
		CreatedAt:           r.CreatedAt,
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

// NewVMFilterOptionsFromModel converts a models.VMFilterOptions to the API type.
func NewVMFilterOptionsFromModel(m models.VMFilterOptions) VMFilterOptionsResponse {
	return VMFilterOptionsResponse{
		Clusters:          m.Clusters,
		Datacenters:       m.Datacenters,
		ConcernLabels:     m.ConcernLabels,
		ConcernCategories: m.ConcernCategories,
		Applications:      m.Applications,
	}
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
