package v1

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kubev2v/migration-planner-common/estimations/estimation"
	"github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

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

// NewVirtualMachineDetailFromModel converts a models.VM to a v1 VirtualMachineDetail.
func NewVirtualMachineDetailFromModel(vm models.VM) VirtualMachineDetail {
	details := VirtualMachineDetail{
		NicCount:        vm.NICCount,
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

	hostNICs := make([]HostPhysicalNIC, 0, len(vm.HostPhysicalNICs))
	for _, nic := range vm.HostPhysicalNICs {
		hostNICs = append(hostNICs, HostPhysicalNIC{Id: nic.ID, LinkSpeedMbps: nic.LinkSpeedMbps})
	}
	details.HostPhysicalNics = &hostNICs

	if vm.Annotation != "" {
		details.Annotation = &vm.Annotation
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
	if vm.InspectionStatus.State != models.InspectionStateNotStarted {
		s := NewInspectionStatus(vm.InspectionStatus)
		details.InspectionStatus = &s
	}
	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled
	if len(vm.Labels) > 0 {
		details.Labels = &vm.Labels
	}
	metadata := vm.Metadata
	if metadata == nil {
		metadata = make(map[string][]string)
	}
	details.Metadata = &metadata

	for _, d := range vm.Disks {
		capacityBytes := d.Capacity * 1024 * 1024
		disk := VirtualMachineDisk{
			File:            &d.File,
			Capacity:        &capacityBytes,
			Shared:          &d.Shared,
			Rdm:             &d.RDM,
			Bus:             &d.Bus,
			Mode:            &d.Mode,
			ThinProvisioned: d.ThinProvisioned,
			EagerlyScrubbed: d.EagerlyScrubbed,
		}
		if d.RDM && d.RDMBacking != nil {
			backing := RDMBacking{}

			if d.RDMBacking.LunUUID != "" {
				backing.LunUuid = &d.RDMBacking.LunUUID
			}

			if d.RDMBacking.DeviceName != "" {
				backing.DeviceName = &d.RDMBacking.DeviceName
			}

			if d.RDMBacking.BackingUUID != "" {
				backing.BackingUuid = &d.RDMBacking.BackingUUID
			}

			mode := RDMBackingCompatibilityMode(d.RDMBacking.CompatibilityMode)
			if mode == "" {
				mode = Unknown
			}
			backing.CompatibilityMode = &mode

			if d.RDMBacking.HostLun != nil {
				hostLun := RDMHostLun{HostId: d.RDMBacking.HostLun.HostID}

				if d.RDMBacking.HostLun.Key != "" {
					hostLun.Key = &d.RDMBacking.HostLun.Key
				}

				if d.RDMBacking.HostLun.CanonicalName != "" {
					hostLun.CanonicalName = &d.RDMBacking.HostLun.CanonicalName
				}

				if d.RDMBacking.HostLun.LunUUID != "" {
					hostLun.LunUuid = &d.RDMBacking.HostLun.LunUUID
				}

				backing.HostLun = &hostLun
			}

			disk.RdmBacking = &backing
		}
		if d.Key != 0 {
			key := d.Key
			disk.Key = &key
		}
		if d.Datastore != nil {
			datastore := VirtualMachineDatastore{}
			if d.Datastore.ID != "" {
				datastore.Id = &d.Datastore.ID
			}
			if d.Datastore.Name != "" {
				datastore.Name = &d.Datastore.Name
			}
			if d.Datastore.Type != "" {
				datastore.Type = &d.Datastore.Type
			}
			if d.Datastore.FilesystemType != "" {
				datastore.FilesystemType = &d.Datastore.FilesystemType
			}
			if d.Datastore.FilesystemMajorVersion > 0 {
				datastore.FilesystemMajorVersion = &d.Datastore.FilesystemMajorVersion
			}
			if d.Datastore.ProtocolType != "" {
				datastore.ProtocolType = &d.Datastore.ProtocolType
			}
			if d.Datastore.StorageProtocol != "" {
				datastore.StorageProtocol = &d.Datastore.StorageProtocol
			}
			if len(d.Datastore.BackingDeviceIDs) > 0 {
				backingDeviceIDs := append([]string(nil), d.Datastore.BackingDeviceIDs...)
				datastore.BackingDeviceIds = &backingDeviceIDs
			}
			if d.Datastore.Vendor != "" {
				datastore.Vendor = &d.Datastore.Vendor
			}
			if d.Datastore.Model != "" {
				datastore.Model = &d.Datastore.Model
			}
			disk.Datastore = &datastore
		}
		details.Disks = append(details.Disks, disk)
	}

	for _, n := range vm.NICs {
		ipv4 := append([]string{}, n.IPv4Addresses...)
		ipv6 := append([]string{}, n.IPv6Addresses...)
		nic := VirtualMachineNIC{
			Adapter:       &n.Adapter,
			Mac:           &n.MAC,
			Network:       &n.Network,
			Index:         &n.Index,
			VlanId:        &n.VlanID,
			Ipv4Addresses: &ipv4,
			Ipv6Addresses: &ipv6,
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
	case models.CollectorStateCollecting:
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
		message := duckdb_parser.RVToolsFailureMessage

		var processing *duckdb_parser.RVToolsProcessingError
		var validation *duckdb_parser.SchemaValidationError
		var workbook *duckdb_parser.XLSXError

		stage := duckdb_parser.RVToolsStageParsing
		if errors.As(status.Error, &processing) {
			stage = processing.Stage
		}

		switch stage {
		case duckdb_parser.RVToolsStageInitialization:
			message = "Could not start processing the RVTools file"
		case duckdb_parser.RVToolsStageParsing:
			switch {
			case errors.As(status.Error, &validation):
				if text := validation.Error(); text != "" {
					message = text
				}
			case errors.As(status.Error, &workbook):
				message = workbook.Error()
			}
		}

		c.Error = &message
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

	if status.Details != "" {
		s.Details = &status.Details
	}
	if status.Error != nil {
		err := status.Error.Error()
		s.Error = &err
	}

	return s
}

// NewRightsizingClusterUtilizationFromModel converts a models.RightsizingClusterUtilization to the API type.
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
	if g.VmCount > 0 {
		group.VmCount = &g.VmCount
	}
	return group
}

func NewVCenterInventory(m models.VCenterInventory) (InventoryResponse, error) {
	var inventory v1alpha1.Inventory
	if err := json.Unmarshal(m.Data, &inventory); err != nil {
		return InventoryResponse{}, err
	}

	resp := InventoryResponse{
		Data:     inventory,
		Vcenters: make([]SubResource, 0, len(m.VcenterIDs)),
	}

	for _, id := range m.VcenterIDs {
		resp.Vcenters = append(resp.Vcenters, SubResource{Id: id})
	}

	return resp, nil
}

func NewMigrationComplexityResponseFromModel(result models.MigrationComplexityResult) MigrationComplexityResponse {
	response := MigrationComplexityResponse{
		ComplexityByDisk:   make([]ComplexityDiskScoreEntry, len(result.ComplexityByDisk)),
		ComplexityByOS:     make([]ComplexityOSScoreEntry, len(result.ComplexityByOS)),
		ComplexityByOSName: make([]ComplexityOSNameEntry, len(result.ComplexityByOSName)),
		DiskSizeRatings:    result.DiskSizeRatings,
		OsRatings:          result.OSRatings,
	}

	for i, entry := range result.ComplexityByDisk {
		response.ComplexityByDisk[i] = ComplexityDiskScoreEntry{
			Score:       entry.Score,
			VmCount:     entry.VMCount,
			TotalSizeTB: entry.TotalSizeTB,
		}
	}

	for i, entry := range result.ComplexityByOS {
		response.ComplexityByOS[i] = ComplexityOSScoreEntry{
			Score:   entry.Score,
			VmCount: entry.VMCount,
		}
	}

	for i, entry := range result.ComplexityByOSName {
		response.ComplexityByOSName[i] = ComplexityOSNameEntry{
			OsName:  entry.Name,
			Score:   entry.Score,
			VmCount: entry.VMCount,
		}
	}

	return response
}

func (r MigrationEstimationRequest) ToModel() models.MigrationEstimationRequestParams {
	var params models.MigrationEstimationRequestParams
	if r.FilterExpression != nil {
		params.Expression = *r.FilterExpression
	}
	if r.EstimationSchema != nil {
		params.Schemas = *r.EstimationSchema
	}
	if r.Params != nil {
		params.Parameters = make([]estimation.Param, 0, len(*r.Params))
		for key, value := range *r.Params {
			params.Parameters = append(params.Parameters, estimation.Param{Key: key, Value: value})
		}
	}
	return params
}

func NewMigrationEstimationResponseFromModel(result models.MigrationEstimationResult) MigrationEstimationResponse {
	return MigrationEstimationResponse{
		Estimation:        newSchemaEstimations(result.Estimation),
		EstimationContext: newEstimationContext(result.EstimationContext),
	}
}

func NewMigrationEstimationByComplexityResponseFromModel(result models.MigrationEstimationByComplexityResult) MigrationEstimationByComplexityResponse {
	entries := make([]OsDiskEstimationEntry, len(result.ComplexityByOSDisk))
	for i, bucket := range result.ComplexityByOSDisk {
		entries[i] = OsDiskEstimationEntry{
			Score:           bucket.Score,
			VmCount:         bucket.VMCount,
			TotalDiskSizeTB: float32(bucket.TotalSizeTB),
		}
		if bucket.Estimation != nil {
			estimates := newSchemaEstimations(bucket.Estimation)
			entries[i].Estimation = &estimates
		}
	}

	matrix := make(map[string]map[string]int, len(result.ComplexityMatrix))
	for osScore, diskScores := range result.ComplexityMatrix {
		inner := make(map[string]int, len(diskScores))
		for diskScore, combinedScore := range diskScores {
			inner[strconv.Itoa(diskScore)] = combinedScore
		}
		matrix[strconv.Itoa(osScore)] = inner
	}

	ctx := newEstimationContext(result.EstimationContext)
	return MigrationEstimationByComplexityResponse{
		ComplexityByOsDisk: entries,
		ComplexityMatrix:   matrix,
		EstimationContext:  &ctx,
	}
}

func (r ClusterRequirementsRequest) ToModel() models.ClusterRequirementsRequestForm {
	form := models.ClusterRequirementsRequestForm{
		WorkerNodeCPU:           r.WorkerNodeCPU,
		WorkerNodeMemory:        r.WorkerNodeMemory,
		WorkerNodeThreads:       r.WorkerNodeThreads,
		ControlPlaneSchedulable: r.ControlPlaneSchedulable,
		ControlPlaneCPU:         r.ControlPlaneCPU,
		ControlPlaneMemory:      r.ControlPlaneMemory,
		HostedControlPlane:      r.HostedControlPlane,
		CompactMode:             r.CompactMode,
	}

	switch r.CpuOverCommitRatio {
	case CpuOneToOne:
		form.CPUOverCommitMultiplier = 1
	case CpuOneToTwo:
		form.CPUOverCommitMultiplier = 2
	case CpuOneToFour:
		form.CPUOverCommitMultiplier = 4
	case CpuOneToSix:
		form.CPUOverCommitMultiplier = 6
	case CpuOneToEight:
		form.CPUOverCommitMultiplier = 8
	}

	switch r.MemoryOverCommitRatio {
	case MemoryOneToOne:
		form.MemoryOverCommitMultiplier = 1
	case MemoryOneToTwo:
		form.MemoryOverCommitMultiplier = 2
	case MemoryOneToFour:
		form.MemoryOverCommitMultiplier = 4
	}

	if r.FilterExpression != nil {
		form.Expression = *r.FilterExpression
	}
	if r.ControlPlaneNodeCount != nil {
		count := int(*r.ControlPlaneNodeCount)
		form.ControlPlaneNodeCount = &count
	}

	return form
}

func NewClusterRequirementsResponseFromModel(result models.ClusterRequirementsResponse) ClusterRequirementsResponse {
	response := ClusterRequirementsResponse{
		ClusterSizing: newClusterSizingFromModel(result.ClusterSizing),
		ResourceConsumption: SizingResourceConsumption{
			Cpu:    result.ResourceConsumption.CPU,
			Memory: result.ResourceConsumption.Memory,
		},
		InventoryTotals: InventoryTotals{
			TotalVMs:    result.InventoryTotals.TotalVMs,
			TotalCPU:    result.InventoryTotals.TotalCPU,
			TotalMemory: result.InventoryTotals.TotalMemory,
		},
	}

	if result.OptimizedSizing != nil {
		sizing := newClusterSizingFromModel(*result.OptimizedSizing)
		response.OptimizedSizing = &sizing
	}
	if result.Savings != nil {
		response.Savings = &Savings{
			NodesSaved:          result.Savings.NodesSaved,
			PercentageReduction: result.Savings.PercentageReduction,
			Description:         result.Savings.Description,
		}
	}
	if result.OptimizationStatus != nil {
		response.OptimizationStatus = &OptimizationStatus{
			Attempted: result.OptimizationStatus.Attempted,
			Reason:    OptimizationStatusReason(result.OptimizationStatus.Reason),
		}
	}
	if result.ResourceConsumption.Limits != nil {
		response.ResourceConsumption.Limits = &SizingResourceLimits{
			Cpu:    result.ResourceConsumption.Limits.CPU,
			Memory: result.ResourceConsumption.Limits.Memory,
		}
	}
	if result.ResourceConsumption.OverCommitRatio != nil {
		response.ResourceConsumption.OverCommitRatio = &SizingOverCommitRatio{
			Cpu:    result.ResourceConsumption.OverCommitRatio.CPU,
			Memory: result.ResourceConsumption.OverCommitRatio.Memory,
		}
	}

	return response
}

func newClusterSizingFromModel(sizing models.ClusterSizing) ClusterSizing {
	return ClusterSizing{
		TotalNodes:           sizing.TotalNodes,
		WorkerNodes:          sizing.WorkerNodes,
		ControlPlaneNodes:    sizing.ControlPlaneNodes,
		FailoverNodes:        sizing.FailoverNodes,
		TotalCPU:             sizing.TotalCPU,
		TotalMemory:          sizing.TotalMemory,
		CpuUtilizationMax:    sizing.CPUUtilizationMax,
		MemoryUtilizationMax: sizing.MemoryUtilizationMax,
		Confidence:           sizing.Confidence,
	}
}

func newSchemaEstimations(results map[string]estimation.Result) map[string]SchemaEstimationResult {
	estimates := make(map[string]SchemaEstimationResult, len(results))
	for schema, result := range results {
		breakdown := make(map[string]EstimationDetail, len(result.Breakdown))
		for name, estimate := range result.Breakdown {
			detail := EstimationDetail{Reason: estimate.Reason}
			if estimate.IsRanged() {
				minDuration := estimate.MinDuration.String()
				maxDuration := estimate.MaxDuration.String()
				detail.MinDuration = &minDuration
				detail.MaxDuration = &maxDuration
			} else {
				duration := estimate.Duration.String()
				detail.Duration = &duration
			}
			breakdown[name] = detail
		}

		estimates[schema] = SchemaEstimationResult{
			MinTotalDuration: result.MinTotalDuration.String(),
			MaxTotalDuration: result.MaxTotalDuration.String(),
			Breakdown:        breakdown,
		}
	}
	return estimates
}

func newEstimationContext(ctx models.EstimationContext) EstimationContext {
	params := make(map[string]float32, len(ctx.Parameters))
	for _, param := range ctx.Parameters {
		switch value := param.Value.(type) {
		case float64:
			params[param.Key] = float32(value)
		case float32:
			params[param.Key] = value
		case int:
			params[param.Key] = float32(value)
		case int64:
			params[param.Key] = float32(value)
		}
	}
	return EstimationContext{Schemas: &ctx.Schemas, Params: &params}
}
