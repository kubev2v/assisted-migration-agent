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
	a.RvtoolsModeEnabled = &m.RVToolsMode
}

// NewVirtualMachineFromSummary converts a models.VirtualMachineSummary to a v2 VirtualMachine.
func NewVirtualMachineFromSummary(vm models.VirtualMachineSummary) VirtualMachine {
	result := VirtualMachine{
		Id:                vm.ID,
		Name:              vm.Name,
		VCenterID:         vm.VCenterID,
		VCenterState:      vm.PowerState,
		Cluster:           vm.Cluster,
		Datacenter:        vm.Datacenter,
		DiskSize:          vm.DiskSize,
		Memory:            int64(vm.Memory),
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
	details.MigrationExcluded = &vm.MigrationExcluded
	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled
	if len(vm.Labels) > 0 {
		details.Labels = &vm.Labels
	}

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

	if vm.Utilization != nil {
		u := NewVmUtilizationDetailsFromModel(*vm.Utilization)
		details.Utilization = &u
	}

	if len(vm.GuestApps) > 0 {
		apps := make([]Process, 0, len(vm.GuestApps))
		for _, g := range vm.GuestApps {
			app := Process{Name: g.Name}
			if g.Version != "" {
				app.Version = &g.Version
			}
			apps = append(apps, app)
		}
		details.Processes = &apps
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

	switch status.State {
	case models.CollectorStateReady:
		c.Status = CollectorStatusStatusReady
	case models.CollectorStateConnecting:
		c.Status = CollectorStatusStatusConnecting
		// TODO: fix rightsizing status
	case models.CollectorStateCollecting, models.CollectorStateRightsizingConnecting: //nolint:staticcheck // deprecated; removed with v1
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateMetricsCollecting:
		c.Status = CollectorStatusMetricsCollecting
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

func NewInspectorStatusFromModel(s models.InspectorStatus) InspectorStatus {
	switch s.State {
	case models.InspectorStateRunning:
		return InspectorStatus{State: InspectorStatusStateRunning}
	default:
		return InspectorStatus{State: InspectorStatusStateReady}
	}
}

// NewForecasterStatusFromModel converts a models.ForecasterStatus to the API type.
func NewForecasterStatusFromModel(s models.ForecasterStatus) ForecasterStatus {
	pairs := make([]ForecastPairStatus, len(s.Pairs))
	for i, p := range s.Pairs {
		pairs[i] = ForecastPairStatus{
			State:           ForecastPairStatusState(p.State),
			PairName:        p.PairName,
			SourceDatastore: p.SourceDatastore,
			TargetDatastore: p.TargetDatastore,
			CompletedRuns:   p.CompletedRuns,
			TotalRuns:       p.TotalRuns,
		}
		if p.Host != "" {
			pairs[i].Host = &p.Host
		}
		if p.PrepBytesTotal > 0 {
			pairs[i].PrepBytesTotal = &p.PrepBytesTotal
		}
		if p.PrepBytesUploaded > 0 {
			pairs[i].PrepBytesUploaded = &p.PrepBytesUploaded
		}
		if p.Error != nil {
			e := p.Error.Error()
			pairs[i].Error = &e
		}
	}
	return ForecasterStatus{
		State: ForecasterStatusState(s.State),
		Pairs: pairs,
	}
}

// NewBenchmarkRunFromModel converts a models.BenchmarkRun to the API type.
func NewBenchmarkRunFromModel(r models.BenchmarkRun) BenchmarkRun {
	run := BenchmarkRun{
		Id:              r.ID,
		SessionId:       r.SessionID,
		PairName:        r.PairName,
		SourceDatastore: r.SourceDS,
		TargetDatastore: r.TargetDS,
		Iteration:       r.Iteration,
		DiskSizeGb:      r.DiskSizeGB,
		DurationSec:     r.DurationSec,
		ThroughputMBps:  r.ThroughputMBps,
		Method:          r.Method,
		CreatedAt:       r.CreatedAt,
	}
	if r.PrepDurationSec > 0 {
		run.PrepDurationSec = &r.PrepDurationSec
	}
	if r.Error != "" {
		run.Error = &r.Error
	}
	return run
}

// NewBenchmarkRunsFromModel converts a slice of models.BenchmarkRun to API types.
func NewBenchmarkRunsFromModel(runs []models.BenchmarkRun) []BenchmarkRun {
	out := make([]BenchmarkRun, len(runs))
	for i, r := range runs {
		out[i] = NewBenchmarkRunFromModel(r)
	}
	return out
}

// NewForecastStatsFromModel converts a models.ForecastStats to the API type.
func NewForecastStatsFromModel(s models.ForecastStats) ForecastStats {
	return ForecastStats{
		PairName:    s.PairName,
		SampleCount: s.SampleCount,
		MeanMBps:    s.MeanMBps,
		MedianMBps:  s.MedianMBps,
		MinMBps:     s.MinMBps,
		MaxMBps:     s.MaxMBps,
		StdDevMBps:  s.StdDevMBps,
		Ci95Lower:   s.CI95Lower,
		Ci95Upper:   s.CI95Upper,
		EstPer1TB: EstimateRange{
			BestCase:  s.EstPer1TB.BestCase.String(),
			Expected:  s.EstPer1TB.Expected.String(),
			WorstCase: s.EstPer1TB.WorstCase.String(),
		},
	}
}

// NewDatastorePairsFromAPI converts API DatastorePairRequest types to model types.
func NewDatastorePairsFromAPI(pairs []DatastorePairRequest) []models.DatastorePair {
	out := make([]models.DatastorePair, len(pairs))
	for i, p := range pairs {
		out[i] = models.DatastorePair{
			Name:            p.Name,
			SourceDatastore: p.SourceDatastore,
			TargetDatastore: p.TargetDatastore,
		}
		if p.Host != nil {
			out[i].Host = *p.Host
		}
	}
	return out
}

// NewDatastoreDetailFromModel converts a models.DatastoreDetail to the API type.
func NewDatastoreDetailFromModel(d models.DatastoreDetail) DatastoreDetail {
	detail := DatastoreDetail{
		Name:       d.Name,
		Type:       d.Type,
		CapacityGb: d.CapacityGB,
		FreeGb:     d.FreeGB,
	}
	if d.StorageVendor != "" {
		detail.StorageVendor = &d.StorageVendor
	}
	if d.StorageModel != "" {
		detail.StorageModel = &d.StorageModel
	}
	if d.StorageArrayID != "" {
		detail.StorageArrayId = &d.StorageArrayID
	}
	if len(d.NAADevices) > 0 {
		detail.NaaDevices = &d.NAADevices
	}
	if len(d.Capabilities) > 0 {
		detail.Capabilities = &d.Capabilities
	}
	return detail
}

// NewDatastoreDetailsFromModel converts a slice of models.DatastoreDetail to API types.
func NewDatastoreDetailsFromModel(details []models.DatastoreDetail) []DatastoreDetail {
	out := make([]DatastoreDetail, len(details))
	for i, d := range details {
		out[i] = NewDatastoreDetailFromModel(d)
	}
	return out
}

// NewPairCapabilityFromModel converts a models.PairCapability to the API type.
func NewPairCapabilityFromModel(p models.PairCapability) PairCapability {
	return PairCapability{
		PairName:        p.PairName,
		SourceDatastore: p.SourceDatastore,
		TargetDatastore: p.TargetDatastore,
		Capabilities:    p.Capabilities,
	}
}

// NewPairCapabilitiesFromModel converts a slice of models.PairCapability to API types.
func NewPairCapabilitiesFromModel(caps []models.PairCapability) []PairCapability {
	out := make([]PairCapability, len(caps))
	for i, c := range caps {
		out[i] = NewPairCapabilityFromModel(c)
	}
	return out
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

func comparisonDiffEntry(e models.ComparisonDiffEntry) ComparisonDiffEntry {
	onlyInA := e.OnlyInA
	onlyInB := e.OnlyInB
	return ComparisonDiffEntry{Delta: e.Delta, OnlyInA: &onlyInA, OnlyInB: &onlyInB}
}

func comparisonDiffEntryDeltaOnly(e models.ComparisonDiffEntry) ComparisonDiffEntry {
	// Clusters has no per-VM identity; OnlyInA/OnlyInB do not apply.
	return ComparisonDiffEntry{Delta: e.Delta}
}

func collectionAggregateFromModel(a models.CollectionAggregate) CollectionAggregate {
	return CollectionAggregate{
		Id:            a.ID,
		CreatedAt:     a.CreatedAt,
		TotalVMs:      a.TotalVMs,
		Migratable:    a.Migratable,
		NonMigratable: a.NonMigratable,
		Clusters:      a.Clusters,
	}
}

// NewCollectionComparisonSummaryFromModel converts a models.ComparisonSummary to the V2 API type.
func NewCollectionComparisonSummaryFromModel(s models.ComparisonSummary) CollectionComparisonSummary {
	return CollectionComparisonSummary{
		Collections: []CollectionAggregate{collectionAggregateFromModel(s.A), collectionAggregateFromModel(s.B)},
		Diff: struct {
			Clusters      ComparisonDiffEntry `json:"clusters"`
			Migratable    ComparisonDiffEntry `json:"migratable"`
			NonMigratable ComparisonDiffEntry `json:"nonMigratable"`
			TotalVMs      ComparisonDiffEntry `json:"totalVMs"`
		}{
			TotalVMs:      comparisonDiffEntry(s.TotalVMs),
			Migratable:    comparisonDiffEntry(s.Migratable),
			NonMigratable: comparisonDiffEntry(s.NonMigratable),
			Clusters:      comparisonDiffEntryDeltaOnly(s.Clusters),
		},
	}
}

// NewCollectionComparisonDiffFromModel converts a models.ComparisonDiff to the V2 API type.
func NewCollectionComparisonDiffFromModel(d models.ComparisonDiff) CollectionComparisonDiff {
	toPage := func(p models.ComparisonDiffPage) ComparisonDiffPage {
		vmIds := p.VMIDs
		if vmIds == nil {
			vmIds = []string{}
		}
		return ComparisonDiffPage{
			Total:     p.Total,
			Page:      p.Page,
			PageCount: p.PageCount,
			VmIds:     vmIds,
		}
	}
	return CollectionComparisonDiff{
		Dimension: CollectionComparisonDiffDimension(d.Dimension),
		OnlyInA:   toPage(d.OnlyInA),
		OnlyInB:   toPage(d.OnlyInB),
	}
}
