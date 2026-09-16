// Package filter provides VM/vCenter-specific filter field mappers for the generic filter DSL.
//
// This package contains MapFunc implementations that map VM-related filter field names
// to their corresponding SQL column references in the assisted-migration-agent database schema.
package filter

import (
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// DefaultMapper maps VM-related filter field names to SQL column references in the
// flat filter subquery (see internal/store VMStore). This is the primary mapper for
// VM filtering operations.
//
// Flat names reference vinfo columns; dotted names reference joined tables:
//
// vinfo (v) — flat names:
//
//	id, name, folder_id, folder, host, smbios_uuid, vm_uuid, firmware,
//	powerstate (alias: status), connection_state, ft_state, cpus, memory,
//	os_config, os_tools, dns_name, ip_address, storage_used, template,
//	cbt, enable_uuid, datacenter, cluster, hw_version, total_disk_capacity,
//	provisioned, resource_pool, labels, groups
//
// vdisk (dk) — disk.* prefix:
//
//	disk.key, disk.path, disk.capacity, disk.sharing, disk.raw,
//	disk.shared_bus, disk.mode, disk.thin, disk.controller, disk.label
//
// concerns (c) — concern.* prefix:
//
//	concern.label, concern.category, concern.assessment
//
// vm_inspection_status (i) — inspection.* prefix:
//
//	inspection.status, inspection.error
//
// vm_inspection_concerns (ic) — inspection_concern.* prefix (latest run only in filter subquery):
//
//	inspection_concern.label, inspection_concern.category, inspection_concern.msg
//
// vcpu (cpu) — cpu.* prefix:
//
//	cpu.hot_add, cpu.hot_remove, cpu.sockets, cpu.cores_per_socket
//
// vmemory (mem) — mem.* prefix:
//
//	mem.hot_add, mem.ballooned
//
// vnetwork (net) — net.* prefix:
//
//	net.network, net.mac, net.nic_label, net.adapter, net.switch,
//	net.connected, net.starts_connected, net.type, net.ipv4, net.ipv6,
//	net.cluster
//
// vdatastore (ds) — datastore.* prefix:
//
//	datastore.name, datastore.hosts, datastore.address, datastore.object_id,
//	datastore.free, datastore.mha, datastore.capacity, datastore.type
//
// rightsizing_vm_utilization (utilization) — utilization.* prefix:
//
//	utilization.provisioned_cpus, utilization.provisioned_memory, utilization.provisioned_disk,
//	utilization.cpu_avg, utilization.cpu_max, utilization.cpu_latest,
//	utilization.mem_avg, utilization.mem_max, utilization.mem_latest,
//	utilization.disk, utilization.confidence
//
// vm_applications (va) — application.* prefix:
//
//	application, application.name, application.description
var DefaultMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	// vinfo (v) — string fields
	case "id":
		return `v."VM ID"`, filter.StringField, nil
	case "name":
		return `v."VM"`, filter.StringField, nil
	case "folder_id":
		return `v."Folder ID"`, filter.StringField, nil
	case "folder":
		return `v."Folder"`, filter.StringField, nil
	case "host":
		return `v."Host"`, filter.StringField, nil
	case "smbios_uuid":
		return `v."SMBIOS UUID"`, filter.StringField, nil
	case "vm_uuid":
		return `v."VM UUID"`, filter.StringField, nil
	case "firmware":
		return `v."Firmware"`, filter.StringField, nil
	case "powerstate", "status":
		return `v."Powerstate"`, filter.StringField, nil
	case "connection_state":
		return `v."Connection state"`, filter.StringField, nil
	case "ft_state":
		return `v."FT State"`, filter.StringField, nil
	case "os_config":
		return `v."OS according to the configuration file"`, filter.StringField, nil
	case "os_tools":
		return `v."OS according to the VMware Tools"`, filter.StringField, nil
	case "dns_name":
		return `v."DNS Name"`, filter.StringField, nil
	case "ip_address":
		return `v."Primary IP Address"`, filter.StringField, nil
	case "hw_version":
		return `v."HW version"`, filter.StringField, nil
	case "resource_pool":
		return `v."Resource pool"`, filter.StringField, nil
	case "datacenter":
		return `v."Datacenter"`, filter.StringField, nil
	case "cluster":
		return `v."Cluster"`, filter.StringField, nil

	// vinfo (v) — numeric fields
	case "cpus":
		return `v."CPUs"`, filter.NumericField, nil
	case "memory":
		return `v."Memory"`, filter.NumericField, nil
	case "storage_used":
		return `v."In Use MiB"`, filter.NumericField, nil
	case "total_disk_capacity":
		return `d.total_disk`, filter.NumericField, nil
	case "provisioned":
		return `v."Provisioned MiB"`, filter.NumericField, nil
	case "issues_count":
		return `cc."issues_count"`, filter.NumericField, nil

	// vinfo (v) — boolean fields
	case "template":
		return `v."Template"`, filter.BooleanField, nil
	case "cbt":
		return `v."CBT"`, filter.BooleanField, nil
	case "enable_uuid":
		return `v."EnableUUID"`, filter.BooleanField, nil
	case "migratable":
		return `(COALESCE(crit.critical_count, 0) = 0)`, filter.BooleanField, nil
	case "migration_excluded":
		return `v."migration_excluded"`, filter.BooleanField, nil

	// vinfo (v) — array fields
	case "labels":
		return `v."labels"`, filter.ArrayField, nil
	case "groups":
		return `g.groups`, filter.ArrayField, nil

	// vdisk (dk) — disk.* prefix
	case "disk.path":
		return `dk."Disk Path"`, filter.StringField, nil
	case "disk.sharing":
		return `dk."Sharing mode"`, filter.StringField, nil
	case "disk.shared_bus":
		return `dk."Shared Bus"`, filter.StringField, nil
	case "disk.mode":
		return `dk."Disk Mode"`, filter.StringField, nil
	case "disk.controller":
		return `dk."Controller"`, filter.StringField, nil
	case "disk.label":
		return `dk."Label"`, filter.StringField, nil
	case "disk.key":
		return `dk."Disk Key"`, filter.NumericField, nil
	case "disk.capacity":
		return `dk."Capacity MiB"`, filter.NumericField, nil
	case "disk.raw":
		return `dk."Raw"`, filter.BooleanField, nil
	case "disk.thin":
		return `dk."Thin"`, filter.BooleanField, nil

	// concerns (c) — concern.* prefix
	case "concern.label":
		return `c."Label"`, filter.StringField, nil
	case "concern.category":
		return `c."Category"`, filter.StringField, nil
	case "concern.assessment":
		return `c."Assessment"`, filter.StringField, nil

	// vm_inspection_status (i) — inspection.* prefix
	case "inspection.status":
		return `i.status`, filter.StringField, nil
	case "inspection.error":
		return `i.error`, filter.StringField, nil

	// vm_inspection_concerns (ic) — inspection_concern.* prefix
	case "inspection_concern.label":
		return `ic.label`, filter.StringField, nil
	case "inspection_concern.category":
		return `ic.category`, filter.StringField, nil
	case "inspection_concern.msg":
		return `ic.msg`, filter.StringField, nil

	// vcpu (cpu) — cpu.* prefix
	case "cpu.sockets":
		return `cpu."Sockets"`, filter.NumericField, nil
	case "cpu.cores_per_socket":
		return `cpu."Cores p/s"`, filter.NumericField, nil
	case "cpu.hot_add":
		return `cpu."Hot Add"`, filter.BooleanField, nil
	case "cpu.hot_remove":
		return `cpu."Hot Remove"`, filter.BooleanField, nil

	// vmemory (mem) — mem.* prefix
	case "mem.ballooned":
		return `mem."Ballooned"`, filter.NumericField, nil
	case "mem.hot_add":
		return `mem."Hot Add"`, filter.BooleanField, nil

	// vnetwork (net) — net.* prefix
	case "net.network":
		return `net."Network"`, filter.StringField, nil
	case "net.mac":
		return `net."Mac Address"`, filter.StringField, nil
	case "net.nic_label":
		return `net."NIC label"`, filter.StringField, nil
	case "net.adapter":
		return `net."Adapter"`, filter.StringField, nil
	case "net.switch":
		return `net."Switch"`, filter.StringField, nil
	case "net.type":
		return `net."Type"`, filter.StringField, nil
	case "net.ipv4":
		return `net."IPv4 Address"`, filter.StringField, nil
	case "net.ipv6":
		return `net."IPv6 Address"`, filter.StringField, nil
	case "net.cluster":
		return `net."Cluster"`, filter.StringField, nil
	case "net.connected":
		return `net."Connected"`, filter.BooleanField, nil
	case "net.starts_connected":
		return `net."Starts Connected"`, filter.BooleanField, nil

	// vdatastore (ds) — datastore.* prefix
	case "datastore.name":
		return `ds."Name"`, filter.StringField, nil
	case "datastore.address":
		return `ds."Address"`, filter.StringField, nil
	case "datastore.object_id":
		return `ds."Object ID"`, filter.StringField, nil
	case "datastore.mha":
		return `ds."MHA"`, filter.StringField, nil
	case "datastore.type":
		return `ds."Type"`, filter.StringField, nil
	case "datastore.hosts":
		return `ds."Hosts"`, filter.NumericField, nil
	case "datastore.free":
		return `ds."Free MiB"`, filter.NumericField, nil
	case "datastore.capacity":
		return `ds."Capacity MiB"`, filter.NumericField, nil

	// rightsizing_vm_utilization (utilization) — utilization.* prefix
	case "utilization.provisioned_cpus":
		return "utilization.provisioned_cpus", filter.NumericField, nil
	case "utilization.provisioned_memory":
		return "utilization.provisioned_memory_mb", filter.NumericField, nil
	case "utilization.provisioned_disk":
		return "utilization.provisioned_disk_kb", filter.NumericField, nil
	case "utilization.cpu_avg":
		return "utilization.cpu_avg_pct", filter.NumericField, nil
	case "utilization.cpu_max":
		return "utilization.cpu_max_pct", filter.NumericField, nil
	case "utilization.cpu_latest":
		return "utilization.cpu_latest_pct", filter.NumericField, nil
	case "utilization.mem_avg":
		return "utilization.mem_avg_pct", filter.NumericField, nil
	case "utilization.mem_max":
		return "utilization.mem_max_pct", filter.NumericField, nil
	case "utilization.mem_latest":
		return "utilization.mem_latest_pct", filter.NumericField, nil
	case "utilization.disk":
		return "utilization.disk_pct", filter.NumericField, nil
	case "utilization.confidence":
		return "utilization.confidence_pct", filter.NumericField, nil

	// vm_applications (va) — application.* prefix
	case "application", "application.name":
		return `va.app_name`, filter.StringField, nil
	case "application.description":
		return `va.app_desc`, filter.StringField, nil

	default:
		return "", 0, fmt.Errorf("unknown filter field: %s", name)
	}
}

// GroupMapper maps group-related filter field names to SQL column references
// in the groups table.
//
// Supported fields:
//   - name: group name
//   - description: group description
//   - filter: group filter expression
var GroupMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	case "name":
		return "name", filter.StringField, nil
	case "description":
		return "description", filter.StringField, nil
	case "filter":
		return "filter", filter.StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown group filter field: %s", name)
	}
}

// ClusterMapper maps cluster-related filter field names to SQL column references.
//
// Supported fields:
//   - cluster_id: cluster identifier
//   - cluster_name: cluster name
var ClusterMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch name {
	case "cluster_id":
		return "cluster_id", filter.StringField, nil
	case "cluster_name":
		return "cluster_name", filter.StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown cluster filter field: %s", name)
	}
}

// CollectionMapper maps collection DSL field names to SQL column names.
// Column name strings must stay in sync with constants in internal/store/collection.go.
//
// Supported fields:
//   - id: collection ID (numeric)
//   - vcenter_id: vCenter identifier
//   - vcenter: vCenter name
//   - state: collection state
//   - active: whether collection is active
//   - started_at, finished_at, created_at, updated_at: timestamp fields
//   - error: error message
var CollectionMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	case "id":
		// BIGINT column — NumericField allows range comparisons (>, <, >=, <=).
		return "id", filter.NumericField, nil
	case "vcenter_id":
		return "vcenter_id", filter.StringField, nil
	case "vcenter":
		return "vcenter", filter.StringField, nil
	case "state":
		return "state", filter.StringField, nil
	case "active":
		return "active", filter.BooleanField, nil
	case "started_at", "finished_at", "created_at", "updated_at":
		// TIMESTAMP columns — AnyField skips type validation so both string-literal
		// equality and ordering comparisons work via DuckDB's implicit casting.
		return name, filter.AnyField, nil
	case "error":
		return "error", filter.StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown collection filter field: %s", name)
	}
}

var ApplicationMapper filter.MapFunc = func(name string) (string, filter.FieldType, error) {
	switch strings.ToLower(name) {
	case "name":
		return "app_name", filter.StringField, nil
	case "description":
		return "app_desc", filter.StringField, nil
	case "vm_id":
		return "vm_id", filter.StringField, nil
	case "vm_name":
		return "vm_name", filter.StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown application filter field: %s", name)
	}
}

// ParseWithDefaultMap parses a filter expression using the DefaultMapper (VM fields).
// This is a convenience wrapper around filter.Parse() for VM filtering operations.
func ParseWithDefaultMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, DefaultMapper)
}

// ParseWithGroupMap parses a filter expression using the GroupMapper (group fields).
// This is a convenience wrapper around filter.Parse() for group filtering operations.
func ParseWithGroupMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, GroupMapper)
}

// ParseWithClusterMap parses a filter expression using the ClusterMapper (cluster fields).
// This is a convenience wrapper around filter.Parse() for cluster filtering operations.
func ParseWithClusterMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, ClusterMapper)
}

// ParseWithCollectionMap parses a filter expression using the CollectionMapper (collection fields).
// This is a convenience wrapper around filter.Parse() for collection filtering operations.
func ParseWithCollectionMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, CollectionMapper)
}

// ParseWithApplicationMap parses a filter expression using the ApplicationMapper (collection fields).
// This is a convenience wrapper around filter.Parse() for application filtering operations.
func ParseWithApplicationMap(src []byte) (sq.Sqlizer, error) {
	return filter.Parse(src, ApplicationMapper)
}
