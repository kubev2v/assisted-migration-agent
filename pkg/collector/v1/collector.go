package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	vspheremodel "github.com/kubev2v/forklift/pkg/controller/provider/model/vsphere"
	web "github.com/kubev2v/forklift/pkg/controller/provider/web/vsphere"
	libmodel "github.com/kubev2v/forklift/pkg/lib/inventory/model"
	apiplanner "github.com/kubev2v/migration-planner/api/v1alpha1"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/util"
	"github.com/kubev2v/assisted-migration-agent/pkg/collector"
)

var vendorMap = map[string]string{
	"NETAPP":   "NetApp",
	"EMC":      "Dell EMC",
	"PURE":     "Pure Storage",
	"3PARDATA": "HPE",
	"ATA":      "ATA",
	"DELL EMC": "Dell EMC",
	"DELL":     "Dell",
	"HPE":      "HPE",
	"IBM":      "IBM",
	"HITACHI":  "Vantara",
	"CISCO":    "Cisco",
	"FUJITSU":  "Fujitsu",
	"LENOVO":   "Lenovo",
}

// Builder builds inventory from forklift collector data.
type Builder struct {
	opaPoliciesDir string
	store          *store.Store
}

// NewBuilder creates a new inventory builder.
func NewBuilder(s *store.Store, opaPoliciesDir string) *Builder {
	return &Builder{
		opaPoliciesDir: opaPoliciesDir,
		store:          s,
	}
}

func (b *Builder) Process(ctx context.Context, c collector.Collector) error {
	zap.S().Named("inventory").Info("Building inventory from forklift collector")

	db := c.DB()

	// List VMs
	vms := &[]vspheremodel.VM{}
	err := db.List(vms, libmodel.FilterOptions{Detail: 1, Predicate: libmodel.Eq("IsTemplate", false)})
	if err != nil {
		return fmt.Errorf("failed to list VMs: %w", err)
	}
	zap.S().Named("inventory").Infof("Found %d VMs", len(*vms))

	// List Hosts
	hosts := &[]vspheremodel.Host{}
	err = db.List(hosts, libmodel.FilterOptions{Detail: 1})
	if err != nil {
		return fmt.Errorf("failed to list hosts: %w", err)
	}
	zap.S().Named("inventory").Infof("Found %d hosts", len(*hosts))

	// List Datacenters
	datacenters := &[]vspheremodel.Datacenter{}
	if err := db.List(datacenters, libmodel.FilterOptions{Detail: 1}); err != nil {
		return fmt.Errorf("failed to list datacenters: %w", err)
	}
	zap.S().Named("inventory").Infof("Found %d datacenters", len(*datacenters))

	// List Clusters
	clusters := &[]vspheremodel.Cluster{}
	err = db.List(clusters, libmodel.FilterOptions{Detail: 1})
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}
	zap.S().Named("inventory").Infof("Found %d clusters", len(*clusters))

	// Get About
	about := &vspheremodel.About{}
	err = db.Get(about)
	if err != nil {
		return fmt.Errorf("failed to get vCenter info: %w", err)
	}

	// List Datastores
	datastores, err := listDatastoresFromDB(db)
	if err != nil {
		return fmt.Errorf("failed to list datastores: %w", err)
	}

	// List Networks
	networks, err := listNetworksFromDB(db)
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Extract cluster mapping and build helper maps
	clusterMapping, hostIDToPowerState, vmsByCluster := ExtractVSphereClusterIDMapping(*vms, *hosts, *clusters)

	// Create vCenter-level inventory
	apiDatastores, datastoreIndexToName, datastoreMapping := getDatastores(hosts, datastores)
	apiNetworks, networkMapping := getNetworks(networks, db, countVmsByNetwork(*vms))

	infraData := models.InfrastructureData{
		Datastores:            apiDatastores,
		Networks:              apiNetworks,
		HostPowerStates:       getHostPowerStates(*hosts),
		Hosts:                 getHosts(hosts),
		ClustersPerDatacenter: *clustersPerDatacenter(datacenters, db),
		TotalHosts:            len(*hosts),
		TotalDatacenters:      len(*datacenters),
	}
	vcenterInv := CreateBasicInventory(vms, infraData)

	// Run the validation of VMs for vCenter-level
	if err := b.validateVMs(ctx, vms); err != nil {
		zap.S().Named("inventory").Warnf("At least one error during VMs validation: %v", err)
	}

	// Fill the vCenter-level inventory object with more data
	datastoreIDToType := buildDatastoreIDToTypeMap(datastores)
	FillInventoryObjectWithMoreData(vms, vcenterInv, datastoreIDToType)

	// Create per-cluster inventories
	perClusterInventories := make(map[string]*apiplanner.InventoryData)
	for _, clusterID := range clusterMapping.ClusterIDs {
		zap.S().Named("inventory").Debugf("Processing cluster: %s", clusterID)

		clusterVMs := vmsByCluster[clusterID]

		// Filter infrastructure data for this cluster
		clusterInfraData := FilterInfraDataByClusterID(
			infraData,
			clusterID,
			clusterMapping.HostToClusterID,
			clusterVMs,
			datastoreMapping,
			datastoreIndexToName,
			networkMapping,
			hostIDToPowerState,
		)

		// Create cluster inventory
		clusterInv := CreateBasicInventory(&clusterVMs, clusterInfraData)

		if len(clusterVMs) > 0 {
			FillInventoryObjectWithMoreData(&clusterVMs, clusterInv, datastoreIDToType) // Fill cluster inventory with more data
		}

		perClusterInventories[clusterID] = clusterInv
	}

	// Create the full V2 Inventory structure
	inventory := &apiplanner.Inventory{
		VcenterId: about.InstanceUuid,
		Clusters:  make(map[string]apiplanner.InventoryData),
		Vcenter:   vcenterInv,
	}

	// Convert per-cluster inventories to the map format
	for clusterID, clusterInv := range perClusterInventories {
		inventory.Clusters[clusterID] = *clusterInv
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// List Folders for VM storage mapping
	folders := &[]vspheremodel.Folder{}
	if err := db.List(folders, libmodel.FilterOptions{Detail: 1}); err != nil {
		return fmt.Errorf("failed to list folders: %w", err)
	}

	// Store VMs in the database
	if err := b.storeVMs(ctx, vms, hosts, clusters, datacenters, folders); err != nil {
		return fmt.Errorf("failed to store VMs: %w", err)
	}

	// Store the inventory
	invData, err := json.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("failed to marshal the inventory: %v", err)
	}

	if err := b.store.Inventory().Save(ctx, invData); err != nil {
		return err
	}

	zap.S().Named("inventory").Infof("Successfully created inventory with %d clusters", len(perClusterInventories))

	return nil
}

func (b *Builder) validateVMs(ctx context.Context, vms *[]vspheremodel.VM) error {
	opaValidator, err := opa.NewValidatorFromDir(b.opaPoliciesDir)
	if err != nil {
		return fmt.Errorf("failed to initialize OPA validator from %s: %w", b.opaPoliciesDir, err)
	}

	// Validate each VM and append concerns
	for i := range *vms {
		vm := &(*vms)[i]
		concerns, err := opaValidator.ValidateVM(ctx, *vm)
		if err != nil {
			return fmt.Errorf("failed to validate VM %q: %w", vm.Name, err)
		}
		vm.Concerns = append(vm.Concerns, concerns...)
	}

	return nil
}

func (b *Builder) storeVMs(
	ctx context.Context,
	vms *[]vspheremodel.VM,
	hosts *[]vspheremodel.Host,
	clusters *[]vspheremodel.Cluster,
	datacenters *[]vspheremodel.Datacenter,
	folders *[]vspheremodel.Folder,
) error {
	// Build host -> cluster ID mapping
	hostToClusterID := make(map[string]string, len(*hosts))
	for _, host := range *hosts {
		if host.ID != "" && host.Cluster != "" {
			hostToClusterID[host.ID] = host.Cluster
		}
	}

	// Build folder ID -> datacenter ID mapping
	folderToDatacenterID := make(map[string]string, len(*folders))
	for _, folder := range *folders {
		folderToDatacenterID[folder.ID] = folder.Datacenter
	}

	// Build cluster ID -> name and cluster ID -> datacenter ID mappings
	clusterIDToName := make(map[string]string, len(*clusters))
	clusterIDToDatacenterID := make(map[string]string, len(*clusters))
	for _, cluster := range *clusters {
		clusterIDToName[cluster.ID] = cluster.Name
		clusterIDToDatacenterID[cluster.ID] = folderToDatacenterID[cluster.Folder]
	}

	// Build datacenter ID -> name mapping
	datacenterIDToName := make(map[string]string, len(*datacenters))
	for _, dc := range *datacenters {
		datacenterIDToName[dc.ID] = dc.Name
	}

	// Convert forklift VMs to model VMs
	modelVMs := make([]models.VM, 0, len(*vms))
	for _, vm := range *vms {
		clusterID := hostToClusterID[vm.Host]
		clusterName := clusterIDToName[clusterID]
		datacenterID := clusterIDToDatacenterID[clusterID]
		datacenterName := datacenterIDToName[datacenterID]

		modelVMs = append(modelVMs, models.NewVMFromForklift(vm, clusterName, datacenterName))
	}

	// Store in the database
	if err := b.store.VM().Insert(ctx, modelVMs...); err != nil {
		return err
	}

	zap.S().Named("inventory").Infof("Stored %d VMs in database", len(modelVMs))
	return nil
}

// CreateBasicInventory creates a basic inventory object with the provided data.
func CreateBasicInventory(
	vms *[]vspheremodel.VM,
	infraData models.InfrastructureData,
) *apiplanner.InventoryData {
	return &apiplanner.InventoryData{
		Vms: apiplanner.VMs{
			Total:                    len(*vms),
			PowerStates:              map[string]int{},
			OsInfo:                   &map[string]apiplanner.OsInfo{},
			DiskSizeTier:             &map[string]apiplanner.DiskSizeTierSummary{},
			DiskTypes:                &map[string]apiplanner.DiskTypeSummary{},
			DistributionByCpuTier:    &map[string]int{},
			DistributionByMemoryTier: &map[string]int{},
			MigrationWarnings:        apiplanner.MigrationIssues{},
			NotMigratableReasons:     apiplanner.MigrationIssues{},
			CpuCores:                 apiplanner.VMResourceBreakdown{},
			RamGB:                    apiplanner.VMResourceBreakdown{},
			DiskCount:                apiplanner.VMResourceBreakdown{},
			DiskGB:                   apiplanner.VMResourceBreakdown{},
			NicCount:                 &apiplanner.VMResourceBreakdown{},
		},
		Infra: apiplanner.Infra{
			ClustersPerDatacenter: &infraData.ClustersPerDatacenter,
			Datastores:            infraData.Datastores,
			HostPowerStates:       infraData.HostPowerStates,
			Hosts:                 infraData.Hosts,
			TotalHosts:            infraData.TotalHosts,
			TotalDatacenters:      &infraData.TotalDatacenters,
			Networks:              infraData.Networks,
		},
	}
}

// FillInventoryObjectWithMoreData fills inventory with detailed VM data.
func FillInventoryObjectWithMoreData(vms *[]vspheremodel.VM, inv *apiplanner.InventoryData, datastoreIDToType map[string]string) {
	diskGBSet := []int{}
	totalAllocatedVCpus := 0  // For poweredOn VMs
	totalAllocatedMemory := 0 // For poweredOn VMs

	for i := range *vms {
		vm := &(*vms)[i]
		diskGBSet = append(diskGBSet, totalCapacity(vm.Disks))

		(*inv.Vms.DistributionByCpuTier)[cpuTierKey(int(vm.CpuCount))]++
		(*inv.Vms.DistributionByMemoryTier)[memoryTierKey(util.MBToGB(vm.MemoryMB))]++

		if vm.PowerState == "poweredOn" {
			totalAllocatedVCpus += int(vm.CpuCount)
			totalAllocatedMemory += int(vm.MemoryMB)
		}

		migratable, hasWarning := migrationReport(vm.Concerns, inv)

		inv.Vms.OsInfo = updateOsInfo(vm, *inv.Vms.OsInfo)

		inv.Vms.PowerStates[vm.PowerState]++

		// Update total values
		inv.Vms.CpuCores.Total += int(vm.CpuCount)
		inv.Vms.RamGB.Total += util.MBToGB(vm.MemoryMB)
		inv.Vms.DiskCount.Total += len(vm.Disks)
		inv.Vms.DiskGB.Total += totalCapacity(vm.Disks)
		inv.Vms.NicCount.Total += len(vm.NICs)

		// Not Migratable
		if !migratable {
			inv.Vms.CpuCores.TotalForNotMigratable += int(vm.CpuCount)
			inv.Vms.RamGB.TotalForNotMigratable += util.MBToGB(vm.MemoryMB)
			inv.Vms.DiskCount.TotalForNotMigratable += len(vm.Disks)
			inv.Vms.DiskGB.TotalForNotMigratable += totalCapacity(vm.Disks)
			inv.Vms.NicCount.TotalForNotMigratable += len(vm.NICs)
		} else {
			if hasWarning {
				inv.Vms.CpuCores.TotalForMigratableWithWarnings += int(vm.CpuCount)
				inv.Vms.RamGB.TotalForMigratableWithWarnings += util.MBToGB(vm.MemoryMB)
				inv.Vms.DiskCount.TotalForMigratableWithWarnings += len(vm.Disks)
				inv.Vms.DiskGB.TotalForMigratableWithWarnings += totalCapacity(vm.Disks)
				inv.Vms.NicCount.TotalForMigratableWithWarnings += len(vm.NICs)
			} else {
				inv.Vms.CpuCores.TotalForMigratable += int(vm.CpuCount)
				inv.Vms.RamGB.TotalForMigratable += util.MBToGB(vm.MemoryMB)
				inv.Vms.DiskCount.TotalForMigratable += len(vm.Disks)
				inv.Vms.DiskGB.TotalForMigratable += totalCapacity(vm.Disks)
				inv.Vms.NicCount.TotalForMigratable += len(vm.NICs)
			}
		}

		inv.Vms.DiskTypes = updateDiskTypeSummary(vm, *inv.Vms.DiskTypes, datastoreIDToType)
	}

	inv.Vms.DiskSizeTier = diskSizeTier(diskGBSet)

	// calculate the cpu and memory overcommitment ratio
	inv.Infra.CpuOverCommitment = util.FloatPtr(calcOverCommitmentRatio(totalAllocatedVCpus, sumHostsCpu(inv.Infra.Hosts)))
	inv.Infra.MemoryOverCommitment = util.FloatPtr(calcOverCommitmentRatio(totalAllocatedMemory, sumHostsMemory(inv.Infra.Hosts)))
}

func calcOverCommitmentRatio(totalAllocated, totalAvailable int) float64 {
	if totalAvailable == 0 {
		return 0.0
	}
	return util.Round(float64(totalAllocated) / float64(totalAvailable))
}

func sumHostsCpu(hosts *[]apiplanner.Host) int {
	total := 0
	for _, h := range *hosts {
		if h.CpuCores == nil {
			continue
		}
		total += *h.CpuCores
	}
	return total
}

func sumHostsMemory(hosts *[]apiplanner.Host) int {
	total := 0
	for _, h := range *hosts {
		if h.MemoryMB == nil {
			continue
		}
		total += int(*h.MemoryMB)
	}
	return total
}

func updateDiskTypeSummary(vm *vspheremodel.VM, summary map[string]apiplanner.DiskTypeSummary, datastoreIDToType map[string]string) *map[string]apiplanner.DiskTypeSummary {
	seenTypes := make(map[string]bool)

	for _, disk := range vm.Disks {
		diskTypeName := datastoreIDToType[disk.Datastore.ID]

		if diskTypeName == "" {
			continue
		}

		diskTypeSummary := summary[diskTypeName]
		if !seenTypes[diskTypeName] {
			diskTypeSummary.VmCount++
			seenTypes[diskTypeName] = true
		}
		diskTypeSummary.TotalSizeTB += util.BytesToTB(disk.Capacity)
		summary[diskTypeName] = diskTypeSummary
	}

	for k, v := range summary {
		v.TotalSizeTB = util.Round(v.TotalSizeTB)
		summary[k] = v
	}

	return &summary
}

func diskSizeTier(diskGBSet []int) *map[string]apiplanner.DiskSizeTierSummary {
	result := make(map[string]apiplanner.DiskSizeTierSummary)

	for _, diskGB := range diskGBSet {
		diskTB := util.GBToTB(diskGB)
		var tierKey string

		switch {
		case diskTB < 10:
			tierKey = "Easy (0-10TB)"
		case diskTB < 20:
			tierKey = "Medium (10-20TB)"
		case diskTB < 50:
			tierKey = "Hard (20-50TB)"
		default:
			tierKey = "White Glove (>50TB)"
		}

		tier := result[tierKey]
		tier.TotalSizeTB += diskTB
		tier.VmCount++
		result[tierKey] = tier
	}

	for k, v := range result {
		v.TotalSizeTB = util.Round(v.TotalSizeTB)
		result[k] = v
	}

	return &result
}

func memoryTierKey(i int) string {
	switch {
	case i <= 4:
		return "0-4"
	case i <= 16:
		return "5-16"
	case i <= 32:
		return "17-32"
	case i <= 64:
		return "33-64"
	case i <= 128:
		return "65-128"
	case i <= 256:
		return "129-256"
	default:
		return "256+"
	}
}

func cpuTierKey(i int) string {
	switch {
	case i <= 4:
		return "0-4"
	case i <= 8:
		return "5-8"
	case i <= 16:
		return "9-16"
	case i <= 32:
		return "17-32"
	default:
		return "32+"
	}
}

func migrationReport(concern []vspheremodel.Concern, inv *apiplanner.InventoryData) (bool, bool) {
	migratable := true
	hasWarning := false
	for _, result := range concern {
		if result.Category == "Critical" {
			migratable = false
			if i := hasID(inv.Vms.NotMigratableReasons, result.Id); i >= 0 {
				inv.Vms.NotMigratableReasons[i].Count++
			} else {
				inv.Vms.NotMigratableReasons = append(inv.Vms.NotMigratableReasons, apiplanner.MigrationIssue{
					Id:         &result.Id,
					Label:      result.Label,
					Count:      1,
					Assessment: result.Assessment,
				})
			}
		}
		if result.Category == "Warning" {
			hasWarning = true
			if i := hasID(inv.Vms.MigrationWarnings, result.Id); i >= 0 {
				inv.Vms.MigrationWarnings[i].Count++
			} else {
				inv.Vms.MigrationWarnings = append(inv.Vms.MigrationWarnings, apiplanner.MigrationIssue{
					Id:         &result.Id,
					Label:      result.Label,
					Count:      1,
					Assessment: result.Assessment,
				})
			}
		}
	}
	if hasWarning {
		if inv.Vms.TotalMigratableWithWarnings == nil {
			total := 0
			inv.Vms.TotalMigratableWithWarnings = &total
		}
		*inv.Vms.TotalMigratableWithWarnings++
	}
	if migratable {
		inv.Vms.TotalMigratable++
	}
	return migratable, hasWarning
}

func hasID(reasons apiplanner.MigrationIssues, id string) int {
	for i, reason := range reasons {
		if id == *reason.Id {
			return i
		}
	}
	return -1
}

func buildDatastoreIDToTypeMap(datastores *[]vspheremodel.Datastore) map[string]string {
	datastoreIDToType := make(map[string]string)

	for _, ds := range *datastores {
		datastoreIDToType[ds.ID] = ds.Type
	}

	return datastoreIDToType
}

func totalCapacity(disks []vspheremodel.Disk) int {
	total := 0
	for _, d := range disks {
		total += int(d.Capacity)
	}
	return util.BytesToGB(total)
}

func listDatastoresFromDB(db libmodel.DB) (*[]vspheremodel.Datastore, error) {
	datastores := &[]vspheremodel.Datastore{}
	err := db.List(datastores, libmodel.FilterOptions{Detail: 1})
	if err != nil {
		return nil, err
	}
	return datastores, nil
}

func listNetworksFromDB(db libmodel.DB) (*[]vspheremodel.Network, error) {
	networks := &[]vspheremodel.Network{}
	err := db.List(networks, libmodel.FilterOptions{Detail: 1})
	if err != nil {
		return nil, err
	}
	return networks, nil
}

func getHostPowerStates(hosts []vspheremodel.Host) map[string]int {
	states := map[string]int{}

	for _, host := range hosts {
		states[host.Status]++
	}

	return states
}

func getHosts(hosts *[]vspheremodel.Host) *[]apiplanner.Host {
	var l []apiplanner.Host

	for _, host := range *hosts {
		cpuCores := int(host.CpuCores)
		cpuSockets := int(host.CpuSockets)
		var memoryMB *int64
		if host.MemoryBytes > 0 {
			mb := util.ConvertBytesToMB(host.MemoryBytes)
			memoryMB = &mb
		}

		l = append(l, apiplanner.Host{
			Id:         &host.ID,
			Model:      host.Model,
			Vendor:     host.Vendor,
			CpuCores:   &cpuCores,
			CpuSockets: &cpuSockets,
			MemoryMB:   memoryMB,
		})
	}

	return &l
}

func countVmsByNetwork(vms []vspheremodel.VM) map[string]int {
	vmsPerNetwork := make(map[string]int)
	for _, vm := range vms {
		for _, network := range vm.Networks {
			vmsPerNetwork[network.ID]++
		}
	}
	return vmsPerNetwork
}

func getNetworks(networks *[]vspheremodel.Network, db libmodel.DB, vmsPerNetwork map[string]int) (
	apiNetworks []apiplanner.Network,
	networkMapping map[string]string,
) {
	apiNetworks = []apiplanner.Network{}
	networkMapping = make(map[string]string, len(*networks))

	for _, n := range *networks {
		if n.ID != "" && n.Name != "" {
			networkMapping[n.ID] = n.Name
		}

		vlanID := n.VlanId
		dvNet := &vspheremodel.Network{}
		if n.Variant == vspheremodel.NetDvPortGroup {
			dvNet.WithRef(n.DVSwitch)
			_ = db.Get(dvNet)
		}
		apiNetworks = append(apiNetworks, apiplanner.Network{
			Name:     n.Name,
			Type:     apiplanner.NetworkType(getNetworkType(&n)),
			VlanId:   &vlanID,
			Dvswitch: &dvNet.Name,
			VmsCount: util.IntPtr(vmsPerNetwork[n.ID]),
		})
	}

	return apiNetworks, networkMapping
}

func getNetworkType(n *vspheremodel.Network) string {
	switch n.Variant {
	case vspheremodel.NetDvPortGroup:
		return string(apiplanner.Distributed)
	case vspheremodel.NetStandard:
		return string(apiplanner.Standard)
	case vspheremodel.NetDvSwitch:
		return string(apiplanner.Dvswitch)
	default:
		return string(apiplanner.Unsupported)
	}
}

func getDatastores(hosts *[]vspheremodel.Host, datastores *[]vspheremodel.Datastore) (
	apiDatastores []apiplanner.Datastore,
	indexToName map[int]string,
	nameToID map[string]string,
) {
	datastoreToHostIDs := make(map[string][]string)
	for _, host := range *hosts {
		for _, dsRef := range host.Datastores {
			datastoreToHostIDs[dsRef.ID] = append(datastoreToHostIDs[dsRef.ID], host.ID)
		}
	}

	apiDatastores = []apiplanner.Datastore{}
	indexToName = make(map[int]string, len(*datastores))
	nameToID = make(map[string]string, len(*datastores))

	for i, ds := range *datastores {
		indexToName[i] = ds.Name
		if ds.Name != "" && ds.ID != "" {
			nameToID[ds.Name] = ds.ID
		}

		dsVendor, dsModel, dsProtocol := findDataStoreInfo(*hosts, ds.BackingDevicesNames)
		hostIDs := datastoreToHostIDs[ds.ID]
		var hostIDPtr *string
		if len(hostIDs) > 0 {
			joined := strings.Join(hostIDs, ", ")
			hostIDPtr = &joined
		}

		apiDatastores = append(apiDatastores, apiplanner.Datastore{
			TotalCapacityGB:         util.BytesToGB(ds.Capacity),
			FreeCapacityGB:          util.BytesToGB(ds.Free),
			HardwareAcceleratedMove: isHardwareAcceleratedMove(*hosts, ds.BackingDevicesNames),
			Type:                    ds.Type,
			Vendor:                  TransformVendorName(dsVendor),
			Model:                   dsModel,
			ProtocolType:            dsProtocol,
			DiskId:                  getNaa(&ds),
			HostId:                  hostIDPtr,
		})
	}

	return apiDatastores, indexToName, nameToID
}

func findDataStoreInfo(hosts []vspheremodel.Host, names []string) (vendor, model, protocol string) {
	vendor, model, protocol = "N/A", "N/A", "N/A"
	if len(names) == 0 {
		return
	}

	for _, host := range hosts {
		for _, disk := range host.HostScsiDisks {
			if disk.CanonicalName != names[0] {
				continue
			}

			vendor = disk.Vendor

			for _, topology := range host.HostScsiTopology {
				if !util.Contains(topology.ScsiDiskKeys, disk.Key) {
					continue
				}

				hbaKey := topology.HbaKey
				for _, hba := range host.HbaDiskInfo {
					if hba.Key == hbaKey {
						model = hba.Model
						protocol = hba.Protocol
						return
					}
				}
			}
		}
	}
	return
}

func getNaa(ds *vspheremodel.Datastore) string {
	if len(ds.BackingDevicesNames) > 0 {
		return ds.BackingDevicesNames[0]
	}
	return "N/A"
}

func isHardwareAcceleratedMove(hosts []vspheremodel.Host, names []string) bool {
	supported := false
	if len(names) == 0 {
		return supported
	}

	for _, host := range hosts {
		for _, disk := range host.HostScsiDisks {
			if disk.CanonicalName != names[0] {
				continue
			}
		}

		resp, err := http.Get("http://localhost:8080/providers/vsphere/1/hosts/" + host.ID + "?advancedOption=DataMover.HardwareAcceleratedMove")
		if err != nil {
			return supported
		}
		defer func() { _ = resp.Body.Close() }()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return supported
		}

		var hostData web.Host
		err = json.Unmarshal(bodyBytes, &hostData)
		if err != nil {
			return supported
		}

		for _, option := range hostData.AdvancedOptions {
			if option.Key == "DataMover.HardwareAcceleratedMove" {
				supported = option.Value == "1"
				return supported
			}
		}
	}

	return supported
}

// TransformVendorName transforms a vendor name to a standardized format.
func TransformVendorName(vendor string) string {
	raw := strings.TrimSpace(vendor)
	key := strings.ToUpper(raw)

	if transformed, exists := vendorMap[key]; exists {
		return transformed
	}

	return raw
}

func clustersPerDatacenter(datacenters *[]vspheremodel.Datacenter, db libmodel.DB) *[]int {
	var h []int

	folders := &[]vspheremodel.Folder{}
	if err := db.List(folders, libmodel.FilterOptions{Detail: 1}); err != nil {
		return nil
	}

	folderByID := make(map[string]vspheremodel.Folder, len(*folders))
	for _, f := range *folders {
		folderByID[f.ID] = f
	}

	for _, dc := range *datacenters {
		hostFolderID := dc.Clusters.ID
		for _, folder := range *folders {
			if folder.ID == hostFolderID {
				h = append(h, countClustersRecursively(folder, folderByID))
				break
			}
		}
	}

	return &h
}

func countClustersRecursively(folder vspheremodel.Folder, folderByID map[string]vspheremodel.Folder) int {
	count := 0

	for _, child := range folder.Children {
		if child.Kind == vspheremodel.ClusterKind && strings.HasPrefix(child.ID, "domain-c") {
			count++
		}

		if child.Kind == vspheremodel.FolderKind {
			count += countClustersRecursively(folderByID[child.ID], folderByID)
		}
	}

	return count
}
