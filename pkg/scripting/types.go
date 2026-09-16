package scripting

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// ---------------------------------------------------------------------------
// VirtualMachine
// ---------------------------------------------------------------------------

type VirtualMachine struct {
	id                     starlark.String
	name                   starlark.String
	vCenterID              starlark.String
	powerState             starlark.String
	cluster                starlark.String
	datacenter             starlark.String
	cpuCount               starlark.Int
	memoryMB               starlark.Int
	diskSize               starlark.Int
	isMigratable           starlark.Bool
	isTemplate             starlark.Bool
	hasRDMDisk             starlark.Bool
	migrationExcluded      starlark.Bool
	issueCount             starlark.Int
	inspectionStatus       starlark.String
	inspectionConcernCount starlark.Int
	labels                 *starlark.List
	groups                 *starlark.List
	tags                   *starlark.List
	osName                 starlark.String
	firmware               starlark.String
	folder                 starlark.String
	host                   starlark.String
	uuid                   starlark.String
	connectionState        starlark.String
	hostname               starlark.String
	ipAddress              starlark.String
	coresPerSocket         starlark.Int
	storageUsed            starlark.Int
	faultToleranceEnabled  starlark.Bool

	utilizationCpuP95     starlark.Value
	utilizationMemP95     starlark.Value
	utilizationCpuMax     starlark.Value
	utilizationMemMax     starlark.Value
	utilizationDisk       starlark.Value
	utilizationConfidence starlark.Value

	disks              *starlark.List
	inspectionConcerns *starlark.List
	applications       *ApplicationList
	nics               *starlark.List
	issues             *starlark.List
	networks           *starlark.List
}

func NewVirtualMachine(vm models.VM) *VirtualMachine {
	hasRDM := false
	diskElems := make([]starlark.Value, len(vm.Disks))
	for i, d := range vm.Disks {
		if d.RDM {
			hasRDM = true
		}
		diskElems[i] = NewDisk(d)
	}

	concernElems := make([]starlark.Value, len(vm.InspectionConcerns))
	for i, c := range vm.InspectionConcerns {
		concernElems[i] = NewInspectionConcern(c)
	}

	apps := make([]*Application, len(vm.GuestApps))
	for i, a := range vm.GuestApps {
		apps[i] = NewApplication(a)
	}

	nicElems := make([]starlark.Value, len(vm.NICs))
	for i, n := range vm.NICs {
		nicElems[i] = NewNIC(n)
	}

	issueElems := make([]starlark.Value, len(vm.Issues))
	for i, iss := range vm.Issues {
		issueElems[i] = NewIssue(iss)
	}

	gnElems := make([]starlark.Value, len(vm.GuestNetworks))
	for i, gn := range vm.GuestNetworks {
		gnElems[i] = NewGuestNetwork(gn)
	}

	vmStarlark := &VirtualMachine{
		id:                     starlark.String(vm.ID),
		name:                   starlark.String(vm.Name),
		vCenterID:              starlark.String(vm.VCenterID),
		powerState:             starlark.String(vm.PowerState),
		cluster:                starlark.String(vm.Cluster),
		datacenter:             starlark.String(vm.Datacenter),
		cpuCount:               starlark.MakeInt(int(vm.CpuCount)),
		memoryMB:               starlark.MakeInt(int(vm.MemoryMB)),
		diskSize:               starlark.MakeInt64(vm.DiskSize),
		isMigratable:           starlark.Bool(vm.IsMigratable),
		isTemplate:             starlark.Bool(vm.IsTemplate),
		hasRDMDisk:             starlark.Bool(hasRDM),
		migrationExcluded:      starlark.Bool(vm.MigrationExcluded),
		issueCount:             starlark.MakeInt(len(vm.Issues)),
		inspectionStatus:       starlark.String(vm.InspectionStatus.State),
		inspectionConcernCount: starlark.MakeInt(len(vm.InspectionConcerns)),
		labels:                 stringSliceToList(vm.Labels),
		groups:                 stringSliceToList(vm.Groups),
		tags:                   starlark.NewList(nil),
		osName:                 starlark.String(vm.GuestName),
		firmware:               starlark.String(vm.Firmware),
		folder:                 starlark.String(vm.Folder),
		host:                   starlark.String(vm.Host),
		uuid:                   starlark.String(vm.UUID),
		connectionState:        starlark.String(vm.ConnectionState),
		hostname:               starlark.String(vm.HostName),
		ipAddress:              starlark.String(vm.IPAddress),
		coresPerSocket:         starlark.MakeInt(int(vm.CoresPerSocket)),
		storageUsed:            starlark.MakeInt64(vm.StorageUsed),
		faultToleranceEnabled:  starlark.Bool(vm.FaultToleranceEnabled),
		utilizationCpuP95:      starlark.None,
		utilizationMemP95:      starlark.None,
		utilizationCpuMax:      starlark.None,
		utilizationMemMax:      starlark.None,
		utilizationDisk:        starlark.None,
		utilizationConfidence:  starlark.None,
		disks:                  starlark.NewList(diskElems),
		inspectionConcerns:     starlark.NewList(concernElems),
		applications:           NewApplicationList(apps),
		nics:                   starlark.NewList(nicElems),
		issues:                 starlark.NewList(issueElems),
		networks:               starlark.NewList(gnElems),
	}

	if vm.Utilization != nil {
		vmStarlark.utilizationConfidence = starlark.Float(vm.Utilization.Confidence)
		vmStarlark.utilizationCpuP95 = starlark.Float(vm.Utilization.CpuP95)
		vmStarlark.utilizationCpuMax = starlark.Float(vm.Utilization.CpuMax)
		vmStarlark.utilizationMemP95 = starlark.Float(vm.Utilization.MemP95)
		vmStarlark.utilizationMemMax = starlark.Float(vm.Utilization.MemMax)
		vmStarlark.utilizationDisk = starlark.Float(vm.Utilization.Disk)
	}

	return vmStarlark
}

func (v *VirtualMachine) String() string        { return fmt.Sprintf("<VirtualMachine %q>", v.name) }
func (v *VirtualMachine) Type() string          { return "VirtualMachine" }
func (v *VirtualMachine) Freeze()               {}
func (v *VirtualMachine) Truth() starlark.Bool  { return true }
func (v *VirtualMachine) Hash() (uint32, error) { return v.id.Hash() }

func (v *VirtualMachine) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return v.id, nil
	case "name":
		return v.name, nil
	case "vcenter_id":
		return v.vCenterID, nil
	case "power_state":
		return v.powerState, nil
	case "cluster":
		return v.cluster, nil
	case "datacenter":
		return v.datacenter, nil
	case "cpu_count":
		return v.cpuCount, nil
	case "memory_mb":
		return v.memoryMB, nil
	case "disk_size":
		return v.diskSize, nil
	case "is_migratable":
		return v.isMigratable, nil
	case "is_template":
		return v.isTemplate, nil
	case "has_rdm_disk":
		return v.hasRDMDisk, nil
	case "migration_excluded":
		return v.migrationExcluded, nil
	case "issue_count":
		return v.issueCount, nil
	case "inspection_status":
		return v.inspectionStatus, nil
	case "inspection_concern_count":
		return v.inspectionConcernCount, nil
	case "labels":
		return v.labels, nil
	case "groups":
		return v.groups, nil
	case "tags":
		return v.tags, nil
	case "os_name":
		return v.osName, nil
	case "firmware":
		return v.firmware, nil
	case "folder":
		return v.folder, nil
	case "host":
		return v.host, nil
	case "uuid":
		return v.uuid, nil
	case "connection_state":
		return v.connectionState, nil
	case "hostname":
		return v.hostname, nil
	case "ip_address":
		return v.ipAddress, nil
	case "cores_per_socket":
		return v.coresPerSocket, nil
	case "storage_used":
		return v.storageUsed, nil
	case "fault_tolerance_enabled":
		return v.faultToleranceEnabled, nil
	case "utilization_cpu_p95":
		return v.utilizationCpuP95, nil
	case "utilization_mem_p95":
		return v.utilizationMemP95, nil
	case "utilization_cpu_max":
		return v.utilizationCpuMax, nil
	case "utilization_mem_max":
		return v.utilizationMemMax, nil
	case "utilization_disk":
		return v.utilizationDisk, nil
	case "utilization_confidence":
		return v.utilizationConfidence, nil
	case "disks":
		return v.disks, nil
	case "inspection_concerns":
		return v.inspectionConcerns, nil
	case "applications":
		return v.applications, nil
	case "nics":
		return v.nics, nil
	case "issues":
		return v.issues, nil
	case "networks":
		return v.networks, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("VirtualMachine has no .%s attribute", name))
	}
}

func (v *VirtualMachine) AttrNames() []string {
	return []string{
		"applications",
		"cluster",
		"connection_state",
		"cores_per_socket",
		"cpu_count",
		"datacenter",
		"disk_size",
		"disks",
		"fault_tolerance_enabled",
		"firmware",
		"folder",
		"groups",
		"has_rdm_disk",
		"host",
		"hostname",
		"id",
		"vcenter_id",
		"inspection_concern_count",
		"inspection_concerns",
		"inspection_status",
		"ip_address",
		"is_migratable",
		"is_template",
		"issue_count",
		"issues",
		"labels",
		"memory_mb",
		"migration_excluded",
		"name",
		"networks",
		"nics",
		"os_name",
		"power_state",
		"storage_used",
		"tags",
		"utilization_confidence",
		"utilization_cpu_max",
		"utilization_cpu_p95",
		"utilization_disk",
		"utilization_mem_max",
		"utilization_mem_p95",
		"uuid",
	}
}

// ---------------------------------------------------------------------------
// Collection
// ---------------------------------------------------------------------------

type Collection struct {
	id        starlark.String
	timestamp starlark.String

	VirtualMachinesFn func(expression string) (*VirtualMachineList, error)
	InventoryFn       func() (*Inventory, error)
	GroupsFn          func() ([]*Group, error)
}

func (c *Collection) String() string        { return fmt.Sprintf("<Collection %q>", c.id) }
func (c *Collection) Type() string          { return "Collection" }
func (c *Collection) Freeze()               {}
func (c *Collection) Truth() starlark.Bool  { return true }
func (c *Collection) Hash() (uint32, error) { return c.id.Hash() }

func (c *Collection) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return c.id, nil
	case "timestamp":
		return c.timestamp, nil
	case "virtual_machines":
		return starlark.NewBuiltin("virtual_machines", c.virtualMachinesMethod), nil
	case "inventory":
		return starlark.NewBuiltin("inventory", c.inventoryMethod), nil
	case "groups":
		return starlark.NewBuiltin("groups", c.groupsMethod), nil
	case "summary":
		return starlark.NewBuiltin("summary", c.summaryMethod), nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Collection has no .%s attribute", name))
	}
}

func (c *Collection) AttrNames() []string {
	return []string{"groups", "id", "inventory", "summary", "timestamp", "virtual_machines"}
}

func (c *Collection) virtualMachinesMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var expression starlark.String
	if err := starlark.UnpackArgs("virtual_machines", args, kwargs, "expression?", &expression); err != nil {
		return nil, err
	}
	if c.VirtualMachinesFn == nil {
		return nil, fmt.Errorf("virtual_machines() not wired")
	}
	return c.VirtualMachinesFn(string(expression))
}

func (c *Collection) inventoryMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if c.InventoryFn == nil {
		return nil, fmt.Errorf("inventory() not wired")
	}
	return c.InventoryFn()
}

func (c *Collection) groupsMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if c.GroupsFn == nil {
		return nil, fmt.Errorf("groups() not wired")
	}
	groups, err := c.GroupsFn()
	if err != nil {
		return nil, err
	}
	elems := make([]starlark.Value, len(groups))
	for i, g := range groups {
		elems[i] = g
	}
	return starlark.NewList(elems), nil
}

func (c *Collection) summaryMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if c.VirtualMachinesFn == nil {
		return nil, fmt.Errorf("summary() not wired")
	}
	vmList, err := c.VirtualMachinesFn("")
	if err != nil {
		return nil, fmt.Errorf("summary: %w", err)
	}
	total := 0
	migratable := 0
	templates := 0
	poweredOff := 0
	excluded := 0
	issues := 0
	for _, vm := range vmList.vms {
		total++
		if vm.isTemplate.Truth() {
			templates++
			continue
		}
		if vm.powerState != "poweredOn" {
			poweredOff++
			continue
		}
		if vm.migrationExcluded.Truth() {
			excluded++
			continue
		}
		if vm.isMigratable.Truth() {
			migratable++
		}
		ic, _ := vm.issueCount.Int64()
		issues += int(ic)
	}
	d := starlark.NewDict(6)
	d.SetKey(starlark.String("total"), starlark.MakeInt(total))
	d.SetKey(starlark.String("migratable"), starlark.MakeInt(migratable))
	d.SetKey(starlark.String("templates"), starlark.MakeInt(templates))
	d.SetKey(starlark.String("powered_off"), starlark.MakeInt(poweredOff))
	d.SetKey(starlark.String("excluded"), starlark.MakeInt(excluded))
	d.SetKey(starlark.String("issues"), starlark.MakeInt(issues))
	return d, nil
}

// ---------------------------------------------------------------------------
// CollectionList
// ---------------------------------------------------------------------------

type CollectionList struct {
	cols []*Collection
}

func NewCollectionList(cols []*Collection) *CollectionList {
	return &CollectionList{cols: cols}
}

func (cl *CollectionList) String() string {
	return fmt.Sprintf("<CollectionList len=%d>", len(cl.cols))
}
func (cl *CollectionList) Type() string         { return "CollectionList" }
func (cl *CollectionList) Freeze()              {}
func (cl *CollectionList) Truth() starlark.Bool { return len(cl.cols) > 0 }
func (cl *CollectionList) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: CollectionList")
}
func (cl *CollectionList) Len() int                   { return len(cl.cols) }
func (cl *CollectionList) Index(i int) starlark.Value { return cl.cols[i] }

func (cl *CollectionList) Iterate() starlark.Iterator {
	return &collectionListIterator{cols: cl.cols}
}

func (cl *CollectionList) Attr(name string) (starlark.Value, error) {
	switch name {
	case "filter":
		return starlark.NewBuiltin("filter", cl.filterMethod), nil
	case "get":
		return starlark.NewBuiltin("get", cl.getMethod), nil
	case "latest":
		return starlark.NewBuiltin("latest", cl.latestMethod), nil
	case "map":
		return starlark.NewBuiltin("map", cl.mapMethod), nil
	case "reduce":
		return starlark.NewBuiltin("reduce", cl.reduceMethod), nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("CollectionList has no .%s attribute", name))
	}
}

func (cl *CollectionList) AttrNames() []string {
	return []string{"filter", "get", "latest", "map", "reduce"}
}

func (cl *CollectionList) latestMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(cl.cols) == 0 {
		return starlark.None, nil
	}
	latest := cl.cols[0]
	for _, c := range cl.cols[1:] {
		if string(c.timestamp) > string(latest.timestamp) {
			latest = c
		}
	}
	return latest, nil
}

func (cl *CollectionList) getMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("get: requires exactly 1 argument")
	}
	switch arg := args[0].(type) {
	case starlark.String:
		id := string(arg)
		for _, c := range cl.cols {
			if string(c.id) == id {
				return c, nil
			}
		}
		return starlark.None, nil
	case starlark.Callable:
		var filtered []*Collection
		for _, c := range cl.cols {
			result, err := starlark.Call(thread, arg, starlark.Tuple{c}, nil)
			if err != nil {
				return nil, fmt.Errorf("get: %w", err)
			}
			if result.Truth() {
				filtered = append(filtered, c)
			}
		}
		return NewCollectionList(filtered), nil
	default:
		return nil, fmt.Errorf("get: argument must be a string (ID) or callable (predicate), got %s", args[0].Type())
	}
}

func (cl *CollectionList) filterMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("filter", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	var filtered []*Collection
	for _, c := range cl.cols {
		result, err := starlark.Call(thread, fn, starlark.Tuple{c}, nil)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		if result.Truth() {
			filtered = append(filtered, c)
		}
	}
	return NewCollectionList(filtered), nil
}

func (cl *CollectionList) mapMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("map", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	elems := make([]starlark.Value, len(cl.cols))
	for i, c := range cl.cols {
		result, err := starlark.Call(thread, fn, starlark.Tuple{c}, nil)
		if err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		elems[i] = result
	}
	return starlark.NewList(elems), nil
}

func (cl *CollectionList) reduceMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var initial starlark.Value
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("reduce", args, nil, 2, &initial, &fn); err != nil {
		return nil, err
	}
	acc := initial
	for _, c := range cl.cols {
		result, err := starlark.Call(thread, fn, starlark.Tuple{acc, c}, nil)
		if err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}
		acc = result
	}
	return acc, nil
}

type collectionListIterator struct {
	cols []*Collection
	i    int
}

func (it *collectionListIterator) Next(p *starlark.Value) bool {
	if it.i >= len(it.cols) {
		return false
	}
	*p = it.cols[it.i]
	it.i++
	return true
}

func (it *collectionListIterator) Done() {}

var (
	_ starlark.Value     = (*CollectionList)(nil)
	_ starlark.HasAttrs  = (*CollectionList)(nil)
	_ starlark.Indexable = (*CollectionList)(nil)
	_ starlark.Iterable  = (*CollectionList)(nil)
	_ starlark.Sequence  = (*CollectionList)(nil)
)

// ---------------------------------------------------------------------------
// VirtualMachineList
// ---------------------------------------------------------------------------

type VirtualMachineList struct {
	vms          []*VirtualMachine
	collectionID string
	rt           *runtime
}

func NewVirtualMachineList(vms []*VirtualMachine) *VirtualMachineList {
	return &VirtualMachineList{vms: vms}
}

func NewVirtualMachineListWithCollection(vms []*VirtualMachine, collectionID string, rt *runtime) *VirtualMachineList {
	return &VirtualMachineList{vms: vms, collectionID: collectionID, rt: rt}
}

func (l *VirtualMachineList) String() string {
	return fmt.Sprintf("<VirtualMachineList len=%d>", len(l.vms))
}
func (l *VirtualMachineList) Type() string         { return "VirtualMachineList" }
func (l *VirtualMachineList) Freeze()              {}
func (l *VirtualMachineList) Truth() starlark.Bool { return len(l.vms) > 0 }
func (l *VirtualMachineList) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: VirtualMachineList")
}
func (l *VirtualMachineList) Len() int { return len(l.vms) }

func (l *VirtualMachineList) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	if op != syntax.PLUS {
		return nil, nil
	}
	other, ok := y.(*VirtualMachineList)
	if !ok {
		return nil, fmt.Errorf("cannot add VirtualMachineList and %s", y.Type())
	}
	merged := make([]*VirtualMachine, 0, len(l.vms)+len(other.vms))
	if side == starlark.Left {
		merged = append(merged, l.vms...)
		merged = append(merged, other.vms...)
	} else {
		merged = append(merged, other.vms...)
		merged = append(merged, l.vms...)
	}
	return NewVirtualMachineListWithCollection(merged, l.collectionID, l.rt), nil
}

func (l *VirtualMachineList) Index(i int) starlark.Value { return l.vms[i] }

func (l *VirtualMachineList) Iterate() starlark.Iterator {
	return &vmListIterator{vms: l.vms}
}

func (l *VirtualMachineList) Attr(name string) (starlark.Value, error) {
	switch name {
	case "count_by":
		return starlark.NewBuiltin("count_by", l.countByMethod), nil
	case "get":
		return starlark.NewBuiltin("get", l.getMethod), nil
	case "filter":
		return starlark.NewBuiltin("filter", l.filterMethod), nil
	case "first":
		return starlark.NewBuiltin("first", l.firstMethod), nil
	case "group_by":
		return starlark.NewBuiltin("group_by", l.groupByMethod), nil
	case "inventory":
		return starlark.NewBuiltin("inventory", l.inventoryMethod), nil
	case "group_by_cluster":
		return starlark.NewBuiltin("group_by_cluster", l.groupByFieldMethod("cluster")), nil
	case "group_by_datacenter":
		return starlark.NewBuiltin("group_by_datacenter", l.groupByFieldMethod("datacenter")), nil
	case "group_by_power_state":
		return starlark.NewBuiltin("group_by_power_state", l.groupByFieldMethod("power_state")), nil
	case "map":
		return starlark.NewBuiltin("map", l.mapMethod), nil
	case "merge":
		return starlark.NewBuiltin("merge", l.mergeMethod), nil
	case "reduce":
		return starlark.NewBuiltin("reduce", l.reduceMethod), nil
	case "sort":
		return starlark.NewBuiltin("sort", l.sortMethod), nil
	case "sum_by":
		return starlark.NewBuiltin("sum_by", l.sumByMethod), nil
	case "totals":
		return starlark.NewBuiltin("totals", l.totalsMethod), nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("VirtualMachineList has no .%s attribute", name))
	}
}

func (l *VirtualMachineList) AttrNames() []string {
	return []string{"count_by", "filter", "first", "get", "group_by", "group_by_cluster", "group_by_datacenter", "group_by_power_state", "inventory", "map", "merge", "reduce", "sort", "sum_by", "totals"}
}

func (l *VirtualMachineList) getMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var id starlark.String
	if err := starlark.UnpackPositionalArgs("get", args, nil, 1, &id); err != nil {
		return nil, err
	}
	for _, vm := range l.vms {
		if vm.id == id {
			return vm, nil
		}
	}
	return starlark.None, nil
}

func (l *VirtualMachineList) filterMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("filter", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	var filtered []*VirtualMachine
	for _, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		if result.Truth() {
			filtered = append(filtered, vm)
		}
	}
	return NewVirtualMachineListWithCollection(filtered, l.collectionID, l.rt), nil
}

func (l *VirtualMachineList) mapMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("map", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	elems := make([]starlark.Value, len(l.vms))
	for i, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		elems[i] = result
	}
	return starlark.NewList(elems), nil
}

func (l *VirtualMachineList) sortMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	reverse := starlark.Bool(false)
	if err := starlark.UnpackArgs("sort", args, kwargs, "key", &fn, "reverse?", &reverse); err != nil {
		return nil, err
	}
	keys := make([]starlark.Value, len(l.vms))
	for i, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("sort: %w", err)
		}
		keys[i] = result
	}
	indices := make([]int, len(l.vms))
	for i := range indices {
		indices[i] = i
	}
	var cmpErr error
	sortFunc := func(i, j int) bool {
		if cmpErr != nil {
			return false
		}
		ok, err := starlark.CompareDepth(syntax.LT, keys[indices[i]], keys[indices[j]], 4)
		if err != nil {
			cmpErr = err
			return false
		}
		if bool(reverse) {
			return !ok
		}
		return ok
	}
	sort.Slice(indices, sortFunc)
	if cmpErr != nil {
		return nil, fmt.Errorf("sort: %w", cmpErr)
	}
	sorted := make([]*VirtualMachine, len(l.vms))
	for i, idx := range indices {
		sorted[i] = l.vms[idx]
	}
	return NewVirtualMachineListWithCollection(sorted, l.collectionID, l.rt), nil
}

func (l *VirtualMachineList) firstMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("first", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	for _, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("first: %w", err)
		}
		if result.Truth() {
			return vm, nil
		}
	}
	return starlark.None, nil
}

func (l *VirtualMachineList) groupByMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("group_by", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	groups := map[string]*[]*VirtualMachine{}
	keyOrder := []starlark.Value{}
	keyStrings := []string{}
	for _, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("group_by: %w", err)
		}
		keyStr := result.String()
		if _, ok := result.(starlark.String); ok {
			keyStr = string(result.(starlark.String))
		}
		if _, exists := groups[keyStr]; !exists {
			groups[keyStr] = &[]*VirtualMachine{}
			keyOrder = append(keyOrder, result)
			keyStrings = append(keyStrings, keyStr)
		}
		bucket := groups[keyStr]
		*bucket = append(*bucket, vm)
	}
	dict := starlark.NewDict(len(groups))
	for i, key := range keyOrder {
		bucket := groups[keyStrings[i]]
		dict.SetKey(key, NewVirtualMachineListWithCollection(*bucket, l.collectionID, l.rt))
	}
	return dict, nil
}

func (l *VirtualMachineList) reduceMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var initial starlark.Value
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("reduce", args, nil, 2, &initial, &fn); err != nil {
		return nil, err
	}
	acc := initial
	for _, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{acc, vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("reduce: %w", err)
		}
		acc = result
	}
	return acc, nil
}

func (l *VirtualMachineList) groupByFieldMethod(field string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		groups := map[string]*[]*VirtualMachine{}
		keyOrder := []starlark.Value{}
		keyStrings := []string{}
		for _, vm := range l.vms {
			val, err := vm.Attr(field)
			if err != nil {
				return nil, fmt.Errorf("group_by_%s: %w", field, err)
			}
			keyStr := val.String()
			if s, ok := val.(starlark.String); ok {
				keyStr = string(s)
			}
			if _, exists := groups[keyStr]; !exists {
				groups[keyStr] = &[]*VirtualMachine{}
				keyOrder = append(keyOrder, val)
				keyStrings = append(keyStrings, keyStr)
			}
			bucket := groups[keyStr]
			*bucket = append(*bucket, vm)
		}
		dict := starlark.NewDict(len(groups))
		for i, key := range keyOrder {
			bucket := groups[keyStrings[i]]
			dict.SetKey(key, NewVirtualMachineListWithCollection(*bucket, l.collectionID, l.rt))
		}
		return dict, nil
	}
}

func (l *VirtualMachineList) mergeMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	merged := make([]*VirtualMachine, len(l.vms))
	copy(merged, l.vms)
	for _, arg := range args {
		other, ok := arg.(*VirtualMachineList)
		if !ok {
			return nil, fmt.Errorf("merge: expected VirtualMachineList, got %s", arg.Type())
		}
		merged = append(merged, other.vms...)
	}
	return NewVirtualMachineListWithCollection(merged, l.collectionID, l.rt), nil
}

func (l *VirtualMachineList) inventoryMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if l.rt == nil || l.collectionID == "" {
		return nil, fmt.Errorf("inventory: no collection context")
	}
	vmIDs := make([]string, len(l.vms))
	for i, vm := range l.vms {
		vmIDs[i] = string(vm.id)
	}
	data, err := l.rt.BuildInventory(context.Background(), l.collectionID, vmIDs)
	if err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	dict, err := jsonBytesToStarlarkDict(data)
	if err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	return dict, nil
}

func (l *VirtualMachineList) countByMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("count_by", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	keyOrder := []starlark.Value{}
	keyStrings := []string{}
	for _, vm := range l.vms {
		result, err := starlark.Call(thread, fn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("count_by: %w", err)
		}
		keyStr := result.String()
		if s, ok := result.(starlark.String); ok {
			keyStr = string(s)
		}
		if _, exists := counts[keyStr]; !exists {
			keyOrder = append(keyOrder, result)
			keyStrings = append(keyStrings, keyStr)
		}
		counts[keyStr]++
	}
	dict := starlark.NewDict(len(counts))
	for i, key := range keyOrder {
		dict.SetKey(key, starlark.MakeInt(counts[keyStrings[i]]))
	}
	return dict, nil
}

func (l *VirtualMachineList) sumByMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var keyFn, valFn starlark.Callable
	if err := starlark.UnpackPositionalArgs("sum_by", args, nil, 2, &keyFn, &valFn); err != nil {
		return nil, err
	}
	sums := map[string]float64{}
	keyOrder := []starlark.Value{}
	keyStrings := []string{}
	for _, vm := range l.vms {
		keyResult, err := starlark.Call(thread, keyFn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("sum_by key: %w", err)
		}
		valResult, err := starlark.Call(thread, valFn, starlark.Tuple{vm}, nil)
		if err != nil {
			return nil, fmt.Errorf("sum_by value: %w", err)
		}
		var val float64
		switch v := valResult.(type) {
		case starlark.Int:
			i, _ := v.Int64()
			val = float64(i)
		case starlark.Float:
			val = float64(v)
		default:
			return nil, fmt.Errorf("sum_by: expected number, got %s", valResult.Type())
		}
		keyStr := keyResult.String()
		if s, ok := keyResult.(starlark.String); ok {
			keyStr = string(s)
		}
		if _, exists := sums[keyStr]; !exists {
			keyOrder = append(keyOrder, keyResult)
			keyStrings = append(keyStrings, keyStr)
		}
		sums[keyStr] += val
	}
	dict := starlark.NewDict(len(sums))
	for i, key := range keyOrder {
		dict.SetKey(key, starlark.Float(sums[keyStrings[i]]))
	}
	return dict, nil
}

func (l *VirtualMachineList) totalsMethod(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fields *starlark.List
	if err := starlark.UnpackPositionalArgs("totals", args, nil, 1, &fields); err != nil {
		return nil, err
	}
	fieldNames := make([]string, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		s, ok := fields.Index(i).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("totals: field names must be strings, got %s", fields.Index(i).Type())
		}
		fieldNames[i] = string(s)
	}

	sums := make([]float64, len(fieldNames))
	hasFloat := make([]bool, len(fieldNames))

	for _, vm := range l.vms {
		for i, field := range fieldNames {
			val, err := vm.Attr(field)
			if err != nil {
				return nil, fmt.Errorf("totals: %w", err)
			}
			switch v := val.(type) {
			case starlark.Int:
				n, _ := v.Int64()
				sums[i] += float64(n)
			case starlark.Float:
				hasFloat[i] = true
				sums[i] += float64(v)
			}
		}
	}

	dict := starlark.NewDict(len(fieldNames))
	for i, field := range fieldNames {
		var val starlark.Value
		if hasFloat[i] {
			val = starlark.Float(sums[i])
		} else {
			val = starlark.MakeInt64(int64(sums[i]))
		}
		dict.SetKey(starlark.String(field), val)
	}
	return dict, nil
}

func (l *VirtualMachineList) VMs() []*VirtualMachine { return l.vms }

type vmListIterator struct {
	vms []*VirtualMachine
	i   int
}

func (it *vmListIterator) Next(p *starlark.Value) bool {
	if it.i >= len(it.vms) {
		return false
	}
	*p = it.vms[it.i]
	it.i++
	return true
}

func (it *vmListIterator) Done() {}

// ---------------------------------------------------------------------------
// Group
// ---------------------------------------------------------------------------

type Group struct {
	id          starlark.String
	name        starlark.String
	description starlark.String
	filter      starlark.String

	VirtualMachinesFn func() (*VirtualMachineList, error)
}

func NewGroup(g models.Group) *Group {
	return &Group{
		id:          starlark.String(g.ID.String()),
		name:        starlark.String(g.Name),
		description: starlark.String(g.Description),
		filter:      starlark.String(g.Filter),
	}
}

func (g *Group) String() string        { return fmt.Sprintf("<Group %q>", g.name) }
func (g *Group) Type() string          { return "Group" }
func (g *Group) Freeze()               {}
func (g *Group) Truth() starlark.Bool  { return true }
func (g *Group) Hash() (uint32, error) { return g.id.Hash() }

func (g *Group) Attr(name string) (starlark.Value, error) {
	switch name {
	case "id":
		return g.id, nil
	case "name":
		return g.name, nil
	case "description":
		return g.description, nil
	case "filter":
		return g.filter, nil
	case "virtual_machines":
		return starlark.NewBuiltin("virtual_machines", g.virtualMachinesMethod), nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Group has no .%s attribute", name))
	}
}

func (g *Group) AttrNames() []string {
	return []string{"description", "filter", "id", "name", "virtual_machines"}
}

func (g *Group) virtualMachinesMethod(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if g.VirtualMachinesFn == nil {
		return nil, fmt.Errorf("virtual_machines() not wired")
	}
	return g.VirtualMachinesFn()
}

// ---------------------------------------------------------------------------
// Disk
// ---------------------------------------------------------------------------

type Disk struct {
	file      starlark.String
	capacity  starlark.Int
	shared    starlark.Bool
	rdm       starlark.Bool
	bus       starlark.String
	mode      starlark.String
	datastore starlark.String
}

func NewDisk(d models.Disk) *Disk {
	ds := ""
	if start := strings.Index(d.File, "["); start != -1 {
		if end := strings.Index(d.File[start:], "]"); end != -1 {
			ds = d.File[start+1 : start+end]
		}
	}
	return &Disk{
		file:      starlark.String(d.File),
		capacity:  starlark.MakeInt64(d.Capacity),
		shared:    starlark.Bool(d.Shared),
		rdm:       starlark.Bool(d.RDM),
		bus:       starlark.String(d.Bus),
		mode:      starlark.String(d.Mode),
		datastore: starlark.String(ds),
	}
}

func (d *Disk) String() string        { return fmt.Sprintf("<Disk %q>", d.file) }
func (d *Disk) Type() string          { return "Disk" }
func (d *Disk) Freeze()               {}
func (d *Disk) Truth() starlark.Bool  { return true }
func (d *Disk) Hash() (uint32, error) { return d.file.Hash() }

func (d *Disk) Attr(name string) (starlark.Value, error) {
	switch name {
	case "file":
		return d.file, nil
	case "capacity":
		return d.capacity, nil
	case "shared":
		return d.shared, nil
	case "rdm":
		return d.rdm, nil
	case "bus":
		return d.bus, nil
	case "mode":
		return d.mode, nil
	case "datastore":
		return d.datastore, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Disk has no .%s attribute", name))
	}
}

func (d *Disk) AttrNames() []string {
	return []string{"bus", "capacity", "datastore", "file", "mode", "rdm", "shared"}
}

// ---------------------------------------------------------------------------
// InspectionConcern
// ---------------------------------------------------------------------------

type InspectionConcern struct {
	category starlark.String
	label    starlark.String
	message  starlark.String
}

func NewInspectionConcern(c models.VmInspectionConcern) *InspectionConcern {
	return &InspectionConcern{
		category: starlark.String(c.Category),
		label:    starlark.String(c.Label),
		message:  starlark.String(c.Msg),
	}
}

func (c *InspectionConcern) String() string       { return fmt.Sprintf("<InspectionConcern %q>", c.label) }
func (c *InspectionConcern) Type() string         { return "InspectionConcern" }
func (c *InspectionConcern) Freeze()              {}
func (c *InspectionConcern) Truth() starlark.Bool { return true }
func (c *InspectionConcern) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: InspectionConcern")
}

func (c *InspectionConcern) Attr(name string) (starlark.Value, error) {
	switch name {
	case "category":
		return c.category, nil
	case "label":
		return c.label, nil
	case "message":
		return c.message, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("InspectionConcern has no .%s attribute", name))
	}
}

func (c *InspectionConcern) AttrNames() []string {
	return []string{"category", "label", "message"}
}

// ---------------------------------------------------------------------------
// Application
// ---------------------------------------------------------------------------

type Application struct {
	name    starlark.String
	process starlark.String
}

func NewApplication(a models.GuestApp) *Application {
	return &Application{
		name:    starlark.String(a.Name),
		process: starlark.String(a.Version),
	}
}

func (a *Application) String() string        { return fmt.Sprintf("<Application %q>", a.name) }
func (a *Application) Type() string          { return "Application" }
func (a *Application) Freeze()               {}
func (a *Application) Truth() starlark.Bool  { return true }
func (a *Application) Hash() (uint32, error) { return a.name.Hash() }

func (a *Application) Attr(name string) (starlark.Value, error) {
	switch name {
	case "name":
		return a.name, nil
	case "process":
		return a.process, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Application has no .%s attribute", name))
	}
}

func (a *Application) AttrNames() []string {
	return []string{"name", "process"}
}

// ---------------------------------------------------------------------------
// ApplicationList
// ---------------------------------------------------------------------------

type ApplicationList struct {
	apps []*Application
}

func NewApplicationList(apps []*Application) *ApplicationList {
	if apps == nil {
		apps = []*Application{}
	}
	return &ApplicationList{apps: apps}
}

func (l *ApplicationList) String() string {
	return fmt.Sprintf("<ApplicationList len=%d>", len(l.apps))
}
func (l *ApplicationList) Type() string         { return "ApplicationList" }
func (l *ApplicationList) Freeze()              {}
func (l *ApplicationList) Truth() starlark.Bool { return len(l.apps) > 0 }
func (l *ApplicationList) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: ApplicationList")
}
func (l *ApplicationList) Len() int                   { return len(l.apps) }
func (l *ApplicationList) Index(i int) starlark.Value { return l.apps[i] }

func (l *ApplicationList) Iterate() starlark.Iterator {
	return &appListIterator{apps: l.apps}
}

func (l *ApplicationList) Attr(name string) (starlark.Value, error) {
	switch name {
	case "filter":
		return starlark.NewBuiltin("filter", l.filterMethod), nil
	case "map":
		return starlark.NewBuiltin("map", l.mapMethod), nil
	case "any":
		return starlark.NewBuiltin("any", l.anyMethod), nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("ApplicationList has no .%s attribute", name))
	}
}

func (l *ApplicationList) AttrNames() []string {
	return []string{"any", "filter", "map"}
}

func (l *ApplicationList) filterMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("filter", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	var filtered []*Application
	for _, app := range l.apps {
		result, err := starlark.Call(thread, fn, starlark.Tuple{app}, nil)
		if err != nil {
			return nil, fmt.Errorf("filter: %w", err)
		}
		if result.Truth() {
			filtered = append(filtered, app)
		}
	}
	return NewApplicationList(filtered), nil
}

func (l *ApplicationList) mapMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("map", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	elems := make([]starlark.Value, len(l.apps))
	for i, app := range l.apps {
		result, err := starlark.Call(thread, fn, starlark.Tuple{app}, nil)
		if err != nil {
			return nil, fmt.Errorf("map: %w", err)
		}
		elems[i] = result
	}
	return starlark.NewList(elems), nil
}

func (l *ApplicationList) anyMethod(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	var fn starlark.Callable
	if err := starlark.UnpackPositionalArgs("any", args, nil, 1, &fn); err != nil {
		return nil, err
	}
	for _, app := range l.apps {
		result, err := starlark.Call(thread, fn, starlark.Tuple{app}, nil)
		if err != nil {
			return nil, fmt.Errorf("any: %w", err)
		}
		if result.Truth() {
			return starlark.True, nil
		}
	}
	return starlark.False, nil
}

type appListIterator struct {
	apps []*Application
	i    int
}

func (it *appListIterator) Next(p *starlark.Value) bool {
	if it.i >= len(it.apps) {
		return false
	}
	*p = it.apps[it.i]
	it.i++
	return true
}

func (it *appListIterator) Done() {}

// ---------------------------------------------------------------------------
// NIC
// ---------------------------------------------------------------------------

type NIC struct {
	mac     starlark.String
	network starlark.String
	ipv4    starlark.String
	ipv6    starlark.String
	index   starlark.Int
}

func NewNIC(n models.NIC) *NIC {
	return &NIC{
		mac:     starlark.String(n.MAC),
		network: starlark.String(n.Network),
		ipv4:    starlark.String(n.IPv4Address),
		ipv6:    starlark.String(n.IPv6Address),
		index:   starlark.MakeInt(n.Index),
	}
}

func (n *NIC) String() string        { return fmt.Sprintf("<NIC %q>", n.mac) }
func (n *NIC) Type() string          { return "NIC" }
func (n *NIC) Freeze()               {}
func (n *NIC) Truth() starlark.Bool  { return true }
func (n *NIC) Hash() (uint32, error) { return n.mac.Hash() }

func (n *NIC) Attr(name string) (starlark.Value, error) {
	switch name {
	case "mac":
		return n.mac, nil
	case "network":
		return n.network, nil
	case "ipv4":
		return n.ipv4, nil
	case "ipv6":
		return n.ipv6, nil
	case "index":
		return n.index, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("NIC has no .%s attribute", name))
	}
}

func (n *NIC) AttrNames() []string {
	return []string{"index", "ipv4", "ipv6", "mac", "network"}
}

// ---------------------------------------------------------------------------
// Issue
// ---------------------------------------------------------------------------

type Issue struct {
	label       starlark.String
	description starlark.String
	category    starlark.String
}

func NewIssue(i models.Issue) *Issue {
	return &Issue{
		label:       starlark.String(i.Label),
		description: starlark.String(i.Description),
		category:    starlark.String(i.Category),
	}
}

func (i *Issue) String() string        { return fmt.Sprintf("<Issue %q>", i.label) }
func (i *Issue) Type() string          { return "Issue" }
func (i *Issue) Freeze()               {}
func (i *Issue) Truth() starlark.Bool  { return true }
func (i *Issue) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: Issue") }

func (i *Issue) Attr(name string) (starlark.Value, error) {
	switch name {
	case "label":
		return i.label, nil
	case "description":
		return i.description, nil
	case "category":
		return i.category, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Issue has no .%s attribute", name))
	}
}

func (i *Issue) AttrNames() []string {
	return []string{"category", "description", "label"}
}

// ---------------------------------------------------------------------------
// GuestNetwork
// ---------------------------------------------------------------------------

type GuestNetwork_s struct {
	device       starlark.String
	mac          starlark.String
	ip           starlark.String
	prefixLength starlark.Int
	network      starlark.String
}

func NewGuestNetwork(g models.GuestNetwork) *GuestNetwork_s {
	return &GuestNetwork_s{
		device:       starlark.String(g.Device),
		mac:          starlark.String(g.MAC),
		ip:           starlark.String(g.IP),
		prefixLength: starlark.MakeInt(int(g.PrefixLength)),
		network:      starlark.String(g.Network),
	}
}

func (g *GuestNetwork_s) String() string       { return fmt.Sprintf("<GuestNetwork %q>", g.device) }
func (g *GuestNetwork_s) Type() string         { return "GuestNetwork" }
func (g *GuestNetwork_s) Freeze()              {}
func (g *GuestNetwork_s) Truth() starlark.Bool { return true }
func (g *GuestNetwork_s) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: GuestNetwork")
}

func (g *GuestNetwork_s) Attr(name string) (starlark.Value, error) {
	switch name {
	case "device":
		return g.device, nil
	case "mac":
		return g.mac, nil
	case "ip":
		return g.ip, nil
	case "prefix_length":
		return g.prefixLength, nil
	case "network":
		return g.network, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("GuestNetwork has no .%s attribute", name))
	}
}

func (g *GuestNetwork_s) AttrNames() []string {
	return []string{"device", "ip", "mac", "network", "prefix_length"}
}

// ---------------------------------------------------------------------------
// Inventory
// ---------------------------------------------------------------------------

type Inventory struct {
	updatedAt starlark.String
	data      *starlark.Dict
}

func NewInventory(updatedAt string, data *starlark.Dict) *Inventory {
	return &Inventory{updatedAt: starlark.String(updatedAt), data: data}
}

func (inv *Inventory) String() string        { return fmt.Sprintf("<Inventory updated_at=%q>", inv.updatedAt) }
func (inv *Inventory) Type() string          { return "Inventory" }
func (inv *Inventory) Freeze()               {}
func (inv *Inventory) Truth() starlark.Bool  { return true }
func (inv *Inventory) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: Inventory") }

func (inv *Inventory) Attr(name string) (starlark.Value, error) {
	switch name {
	case "updated_at":
		return inv.updatedAt, nil
	case "data":
		return inv.data, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("Inventory has no .%s attribute", name))
	}
}

func (inv *Inventory) AttrNames() []string {
	return []string{"data", "updated_at"}
}

// ---------------------------------------------------------------------------
// ForecastStats
// ---------------------------------------------------------------------------

type ForecastStats struct {
	pairName    starlark.String
	sampleCount starlark.Int
	meanMBps    starlark.Float
	medianMBps  starlark.Float
	minMBps     starlark.Float
	maxMBps     starlark.Float
	stdDevMBps  starlark.Float
	ci95Lower   starlark.Float
	ci95Upper   starlark.Float
}

func NewForecastStats(s models.ForecastStats) *ForecastStats {
	return &ForecastStats{
		pairName:    starlark.String(s.PairName),
		sampleCount: starlark.MakeInt(s.SampleCount),
		meanMBps:    starlark.Float(s.MeanMBps),
		medianMBps:  starlark.Float(s.MedianMBps),
		minMBps:     starlark.Float(s.MinMBps),
		maxMBps:     starlark.Float(s.MaxMBps),
		stdDevMBps:  starlark.Float(s.StdDevMBps),
		ci95Lower:   starlark.Float(s.CI95Lower),
		ci95Upper:   starlark.Float(s.CI95Upper),
	}
}

func (f *ForecastStats) String() string        { return fmt.Sprintf("<ForecastStats %q>", f.pairName) }
func (f *ForecastStats) Type() string          { return "ForecastStats" }
func (f *ForecastStats) Freeze()               {}
func (f *ForecastStats) Truth() starlark.Bool  { return true }
func (f *ForecastStats) Hash() (uint32, error) { return f.pairName.Hash() }

func (f *ForecastStats) Attr(name string) (starlark.Value, error) {
	switch name {
	case "pair_name":
		return f.pairName, nil
	case "sample_count":
		return f.sampleCount, nil
	case "mean_mbps":
		return f.meanMBps, nil
	case "median_mbps":
		return f.medianMBps, nil
	case "min_mbps":
		return f.minMBps, nil
	case "max_mbps":
		return f.maxMBps, nil
	case "std_dev_mbps":
		return f.stdDevMBps, nil
	case "ci95_lower":
		return f.ci95Lower, nil
	case "ci95_upper":
		return f.ci95Upper, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("ForecastStats has no .%s attribute", name))
	}
}

func (f *ForecastStats) AttrNames() []string {
	return []string{
		"ci95_lower", "ci95_upper", "max_mbps", "mean_mbps",
		"median_mbps", "min_mbps", "pair_name", "sample_count", "std_dev_mbps",
	}
}

// ---------------------------------------------------------------------------
// FieldChange (diff)
// ---------------------------------------------------------------------------

type FieldChange struct {
	field    starlark.String
	oldValue starlark.Value
	newValue starlark.Value
}

func NewFieldChange(field string, oldValue, newValue starlark.Value) *FieldChange {
	return &FieldChange{field: starlark.String(field), oldValue: oldValue, newValue: newValue}
}

func (f *FieldChange) String() string        { return fmt.Sprintf("<FieldChange %q>", f.field) }
func (f *FieldChange) Type() string          { return "FieldChange" }
func (f *FieldChange) Freeze()               {}
func (f *FieldChange) Truth() starlark.Bool  { return true }
func (f *FieldChange) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: FieldChange") }

func (f *FieldChange) Attr(name string) (starlark.Value, error) {
	switch name {
	case "field":
		return f.field, nil
	case "old":
		return f.oldValue, nil
	case "new":
		return f.newValue, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("FieldChange has no .%s attribute", name))
	}
}

func (f *FieldChange) AttrNames() []string { return []string{"field", "new", "old"} }

// ---------------------------------------------------------------------------
// CollectionDiff
// ---------------------------------------------------------------------------

type CollectionDiff struct {
	added     *starlark.List
	removed   *starlark.List
	changed   *starlark.List
	unchanged starlark.Int
}

func NewCollectionDiff(added, removed []*VirtualMachine, changed []*VirtualMachineDiff, unchanged int) *CollectionDiff {
	addedElems := make([]starlark.Value, len(added))
	for i, vm := range added {
		addedElems[i] = vm
	}
	removedElems := make([]starlark.Value, len(removed))
	for i, vm := range removed {
		removedElems[i] = vm
	}
	changedElems := make([]starlark.Value, len(changed))
	for i, c := range changed {
		changedElems[i] = c
	}
	return &CollectionDiff{
		added:     starlark.NewList(addedElems),
		removed:   starlark.NewList(removedElems),
		changed:   starlark.NewList(changedElems),
		unchanged: starlark.MakeInt(unchanged),
	}
}

func (d *CollectionDiff) String() string       { return "<CollectionDiff>" }
func (d *CollectionDiff) Type() string         { return "CollectionDiff" }
func (d *CollectionDiff) Freeze()              {}
func (d *CollectionDiff) Truth() starlark.Bool { return true }
func (d *CollectionDiff) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: CollectionDiff")
}

func (d *CollectionDiff) Attr(name string) (starlark.Value, error) {
	switch name {
	case "added":
		return d.added, nil
	case "removed":
		return d.removed, nil
	case "changed":
		return d.changed, nil
	case "unchanged":
		return d.unchanged, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("CollectionDiff has no .%s attribute", name))
	}
}

func (d *CollectionDiff) AttrNames() []string {
	return []string{"added", "changed", "removed", "unchanged"}
}

// ---------------------------------------------------------------------------
// VirtualMachineDiff
// ---------------------------------------------------------------------------

type VirtualMachineDiff struct {
	vmID    starlark.String
	vmA     *VirtualMachine
	vmB     *VirtualMachine
	changes *starlark.List
}

func NewVirtualMachineDiff(vmID string, vmA, vmB *VirtualMachine, changes []*FieldChange) *VirtualMachineDiff {
	elems := make([]starlark.Value, len(changes))
	for i, ch := range changes {
		elems[i] = ch
	}
	return &VirtualMachineDiff{
		vmID:    starlark.String(vmID),
		vmA:     vmA,
		vmB:     vmB,
		changes: starlark.NewList(elems),
	}
}

func (c *VirtualMachineDiff) String() string        { return fmt.Sprintf("<VirtualMachineDiff %q>", c.vmID) }
func (c *VirtualMachineDiff) Type() string          { return "VirtualMachineDiff" }
func (c *VirtualMachineDiff) Freeze()               {}
func (c *VirtualMachineDiff) Truth() starlark.Bool  { return true }
func (c *VirtualMachineDiff) Hash() (uint32, error) { return c.vmID.Hash() }

func (c *VirtualMachineDiff) Attr(name string) (starlark.Value, error) {
	switch name {
	case "vm_id":
		return c.vmID, nil
	case "vm_a":
		return c.vmA, nil
	case "vm_b":
		return c.vmB, nil
	case "changes":
		return c.changes, nil
	default:
		return nil, starlark.NoSuchAttrError(fmt.Sprintf("VirtualMachineDiff has no .%s attribute", name))
	}
}

func (c *VirtualMachineDiff) AttrNames() []string {
	return []string{"changes", "vm_a", "vm_b", "vm_id"}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func float64PtrOrNone(f *float64) starlark.Value {
	if f == nil {
		return starlark.None
	}
	return starlark.Float(*f)
}

func stringSliceToList(ss []string) *starlark.List {
	elems := make([]starlark.Value, len(ss))
	for i, s := range ss {
		elems[i] = starlark.String(s)
	}
	return starlark.NewList(elems)
}

// ---------------------------------------------------------------------------
// Interface guards
// ---------------------------------------------------------------------------

var (
	_ starlark.Value     = (*VirtualMachine)(nil)
	_ starlark.HasAttrs  = (*VirtualMachine)(nil)
	_ starlark.Value     = (*Collection)(nil)
	_ starlark.HasAttrs  = (*Collection)(nil)
	_ starlark.Value     = (*VirtualMachineList)(nil)
	_ starlark.HasAttrs  = (*VirtualMachineList)(nil)
	_ starlark.Indexable = (*VirtualMachineList)(nil)
	_ starlark.Iterable  = (*VirtualMachineList)(nil)
	_ starlark.Sequence  = (*VirtualMachineList)(nil)
	_ starlark.Value     = (*Group)(nil)
	_ starlark.HasAttrs  = (*Group)(nil)
	_ starlark.Value     = (*Disk)(nil)
	_ starlark.HasAttrs  = (*Disk)(nil)
	_ starlark.Value     = (*InspectionConcern)(nil)
	_ starlark.HasAttrs  = (*InspectionConcern)(nil)
	_ starlark.Value     = (*Application)(nil)
	_ starlark.HasAttrs  = (*Application)(nil)
	_ starlark.Value     = (*ApplicationList)(nil)
	_ starlark.HasAttrs  = (*ApplicationList)(nil)
	_ starlark.Indexable = (*ApplicationList)(nil)
	_ starlark.Iterable  = (*ApplicationList)(nil)
	_ starlark.Sequence  = (*ApplicationList)(nil)
	_ starlark.Value     = (*NIC)(nil)
	_ starlark.HasAttrs  = (*NIC)(nil)
	_ starlark.Value     = (*Issue)(nil)
	_ starlark.HasAttrs  = (*Issue)(nil)
	_ starlark.Value     = (*GuestNetwork_s)(nil)
	_ starlark.HasAttrs  = (*GuestNetwork_s)(nil)
	_ starlark.Value     = (*Inventory)(nil)
	_ starlark.HasAttrs  = (*Inventory)(nil)
	_ starlark.Value     = (*ForecastStats)(nil)
	_ starlark.HasAttrs  = (*ForecastStats)(nil)
	_ starlark.Value     = (*FieldChange)(nil)
	_ starlark.HasAttrs  = (*FieldChange)(nil)
	_ starlark.Value     = (*CollectionDiff)(nil)
	_ starlark.HasAttrs  = (*CollectionDiff)(nil)
	_ starlark.Value     = (*VirtualMachineDiff)(nil)
	_ starlark.HasAttrs  = (*VirtualMachineDiff)(nil)
)
