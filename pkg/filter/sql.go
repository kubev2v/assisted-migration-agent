package filter

import (
	"errors"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
)

// MapFunc resolves a filter identifier (e.g. "memory") to a fully qualified
// SQL column reference (e.g. m."Size MB") and its expected FieldType.
// The function should return an error for unknown identifiers.
type MapFunc func(name string) (string, FieldType, error)

var defaultMapFn MapFunc = func(name string) (string, FieldType, error) {
	switch strings.ToLower(name) {
	// vinfo (v_)
	case "id":
		return `v_vm_id`, StringField, nil
	case "name":
		return `v_vm`, StringField, nil
	case "folder_id":
		return `v_folder_id`, StringField, nil
	case "folder":
		return `v_folder`, StringField, nil
	case "host":
		return `v_host`, StringField, nil
	case "smbios_uuid":
		return `v_smbios_uuid`, StringField, nil
	case "vm_uuid":
		return `v_vm_uuid`, StringField, nil
	case "firmware":
		return `v_firmware`, StringField, nil
	case "powerstate", "status":
		return `v_powerstate`, StringField, nil
	case "connection_state":
		return `v_connection_state`, StringField, nil
	case "ft_state":
		return `v_ft_state`, StringField, nil
	case "os_config":
		return `v_os_config`, StringField, nil
	case "os_tools":
		return `v_os_tools`, StringField, nil
	case "dns_name":
		return `v_dns_name`, StringField, nil
	case "ip_address":
		return `v_ip_address`, StringField, nil
	case "hw_version":
		return `v_hw_version`, StringField, nil
	case "resource_pool":
		return `v_resource_pool`, StringField, nil
	case "datacenter":
		return `v_datacenter`, StringField, nil
	case "cluster":
		return `v_cluster`, StringField, nil
	case "cpus":
		return `v_cpus`, NumericField, nil
	case "memory":
		return `v_memory`, NumericField, nil
	case "storage_used":
		return `v_in_use_mib`, NumericField, nil
	case "provisioned":
		return `v_provisioned_mib`, NumericField, nil
	case "template":
		return `v_template`, BooleanField, nil
	case "cbt":
		return `v_cbt`, BooleanField, nil
	case "enable_uuid":
		return `v_enable_uuid`, BooleanField, nil

	// computed aggregates
	case "total_disk_capacity":
		return `total_disk`, NumericField, nil
	case "issues_count":
		return `issues_count`, NumericField, nil
	case "migratable":
		return `migratable`, BooleanField, nil

	// vdisk (dk_)
	case "disk.path":
		return `dk_disk_path`, StringField, nil
	case "disk.sharing":
		return `dk_sharing_mode`, StringField, nil
	case "disk.shared_bus":
		return `dk_shared_bus`, StringField, nil
	case "disk.mode":
		return `dk_disk_mode`, StringField, nil
	case "disk.controller":
		return `dk_controller`, StringField, nil
	case "disk.label":
		return `dk_label`, StringField, nil
	case "disk.key":
		return `dk_disk_key`, NumericField, nil
	case "disk.capacity":
		return `dk_capacity_mib`, NumericField, nil
	case "disk.raw":
		return `dk_raw`, BooleanField, nil
	case "disk.thin":
		return `dk_thin`, BooleanField, nil

	// concerns (c_)
	case "concern.label":
		return `c_label`, StringField, nil
	case "concern.category":
		return `c_category`, StringField, nil
	case "concern.assessment":
		return `c_assessment`, StringField, nil

	// vm_inspection_status (inspection_)
	case "inspection.status":
		return `inspection_status`, StringField, nil
	case "inspection.error":
		return `inspection_error`, StringField, nil

	// vm_inspection_concerns (ic_)
	case "inspection_concern.label":
		return `ic_label`, StringField, nil
	case "inspection_concern.category":
		return `ic_category`, StringField, nil
	case "inspection_concern.msg":
		return `ic_msg`, StringField, nil

	// vcpu (cpu_)
	case "cpu.sockets":
		return `cpu_sockets`, NumericField, nil
	case "cpu.cores_per_socket":
		return `cpu_cores_ps`, NumericField, nil
	case "cpu.hot_add":
		return `cpu_hot_add`, BooleanField, nil
	case "cpu.hot_remove":
		return `cpu_hot_remove`, BooleanField, nil

	// vmemory (mem_)
	case "mem.ballooned":
		return `mem_ballooned`, NumericField, nil
	case "mem.hot_add":
		return `mem_hot_add`, BooleanField, nil

	// vnetwork (net_)
	case "net.network":
		return `net_network`, StringField, nil
	case "net.mac":
		return `net_mac_address`, StringField, nil
	case "net.nic_label":
		return `net_nic_label`, StringField, nil
	case "net.adapter":
		return `net_adapter`, StringField, nil
	case "net.switch":
		return `net_switch`, StringField, nil
	case "net.type":
		return `net_type`, StringField, nil
	case "net.ipv4":
		return `net_ipv4_address`, StringField, nil
	case "net.ipv6":
		return `net_ipv6_address`, StringField, nil
	case "net.cluster":
		return `net_cluster`, StringField, nil
	case "net.connected":
		return `net_connected`, BooleanField, nil
	case "net.starts_connected":
		return `net_starts_connected`, BooleanField, nil

	// vdatastore (ds_)
	case "datastore.name":
		return `ds_name`, StringField, nil
	case "datastore.address":
		return `ds_address`, StringField, nil
	case "datastore.object_id":
		return `ds_object_id`, StringField, nil
	case "datastore.mha":
		return `ds_mha`, BooleanField, nil
	case "datastore.type":
		return `ds_type`, StringField, nil
	case "datastore.hosts":
		return `ds_hosts`, StringField, nil
	case "datastore.free":
		return `ds_free_mib`, NumericField, nil
	case "datastore.capacity":
		return `ds_capacity_mib`, NumericField, nil

	// utilization (u_)
	case "utilization.cpu_avg":
		return `u_cpu_avg_pct`, NumericField, nil
	case "utilization.cpu_p95":
		return `u_cpu_p95_pct`, NumericField, nil
	case "utilization.cpu_max":
		return `u_cpu_max_pct`, NumericField, nil
	case "utilization.cpu_latest":
		return `u_cpu_latest_pct`, NumericField, nil
	case "utilization.mem_avg":
		return `u_mem_avg_pct`, NumericField, nil
	case "utilization.mem_p95":
		return `u_mem_p95_pct`, NumericField, nil
	case "utilization.mem_max":
		return `u_mem_max_pct`, NumericField, nil
	case "utilization.mem_latest":
		return `u_mem_latest_pct`, NumericField, nil
	case "utilization.disk":
		return `u_disk_pct`, NumericField, nil
	case "utilization.confidence":
		return `u_confidence_pct`, NumericField, nil

	default:
		return "", 0, fmt.Errorf("unknown filter field: %s", name)
	}
}

var groupMapFn MapFunc = func(name string) (string, FieldType, error) {
	switch strings.ToLower(name) {
	case "name":
		return "name", StringField, nil
	case "description":
		return "description", StringField, nil
	case "filter":
		return "filter", StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown group filter field: %s", name)
	}
}

var clusterMapFn MapFunc = func(name string) (string, FieldType, error) {
	switch name {
	case "cluster_id":
		return "cluster_id", StringField, nil
	case "cluster_name":
		return "cluster_name", StringField, nil
	default:
		return "", 0, fmt.Errorf("unknown cluster filter field: %s", name)
	}
}

func toSql(expr Expression, mf MapFunc) (sq.Sqlizer, error) {
	switch e := expr.(type) {
	case *binaryExpression:
		if e.Op != and && e.Op != or {
			if v, ok := e.Left.(*varExpression); ok {
				_, fieldType, err := mf(strings.ToLower(v.Name))
				if err != nil {
					return nil, err
				}
				if err := checkValueType(fieldType, e.Right); err != nil {
					return nil, fmt.Errorf("field %q is %s, but got %s value", v.Name, fieldType, e.Right.Type())
				}
			}
		}

		left, err := toSql(e.Left, mf)
		if err != nil {
			return nil, err
		}

		right, err := toSql(e.Right, mf)
		if err != nil {
			return nil, err
		}

		leftSQL, leftArgs, err := left.ToSql()
		if err != nil {
			return nil, err
		}

		rightSQL, rightArgs, err := right.ToSql()
		if err != nil {
			return nil, err
		}

		args := append(leftArgs, rightArgs...)
		switch e.Op {
		case like:
			return sq.Expr(fmt.Sprintf("regexp_matches(%s, %s)", leftSQL, rightSQL), args...), nil
		case notLike:
			return sq.Expr(fmt.Sprintf("NOT regexp_matches(%s, %s)", leftSQL, rightSQL), args...), nil
		case and:
			return sq.And{left, right}, nil
		case or:
			return sq.Or{left, right}, nil
		case like2:
			pattern := fmt.Sprintf("%%%v%%", rightArgs[0])
			return sq.Expr(fmt.Sprintf("(%s %s ?)", leftSQL, e.Op.Sql()), append(leftArgs, pattern)...), nil
		default:
			return sq.Expr(fmt.Sprintf("(%s %s %s)", leftSQL, e.Op.Sql(), rightSQL), args...), nil
		}
	case *varExpression:
		col, _, err := mf(strings.ToLower(e.Name))
		if err != nil {
			return nil, err
		}
		return sq.Expr(col), nil
	case *stringExpression:
		return sq.Expr("?", e.Value), nil
	case *booleanExpression:
		if e.Value {
			return sq.Expr("TRUE"), nil
		}
		return sq.Expr("FALSE"), nil
	case *regexExpression:
		return sq.Expr("?", e.Pattern), nil
	case *quantityExpression:
		var valueInMb float64
		switch e.Unit {
		case KbQuantityUnit:
			valueInMb = e.Value / 1024
		case MbQuantityUnit:
			valueInMb = e.Value
		case GbQuantityUnit:
			valueInMb = e.Value * 1024
		case TbQuantityUnit:
			valueInMb = e.Value * 1024 * 1024
		default:
			valueInMb = e.Value
		}
		return sq.Expr("?", valueInMb), nil
	case *inExpression:
		col, ft, err := mf(strings.ToLower(e.Left.(*varExpression).Name))
		if err != nil {
			return nil, err
		}
		if ft != StringField && ft != AnyField {
			return nil, fmt.Errorf("field %q is %s, but in/not in requires a string field", e.Left.(*varExpression).Name, ft)
		}
		if e.Negated {
			return sq.NotEq{col: e.Values}, nil
		}
		return sq.Eq{col: e.Values}, nil
	default:
		return nil, fmt.Errorf("unknown expression type: %T", expr)
	}
}

// FieldType describes the expected value type for a filter field.
type FieldType int

const (
	// AnyField skips type validation. Use when field types are unknown.
	AnyField FieldType = iota
	StringField
	NumericField
	BooleanField
)

func (f FieldType) String() string {
	switch f {
	case AnyField:
		return "any"
	case StringField:
		return "string"
	case NumericField:
		return "numeric"
	case BooleanField:
		return "boolean"
	default:
		return "unknown"
	}
}

func checkValueType(ft FieldType, value Expression) error {
	switch ft {
	case AnyField:
		return nil
	case StringField:
		switch value.(type) {
		case *stringExpression, *regexExpression:
			return nil
		}
	case NumericField:
		if _, ok := value.(*quantityExpression); ok {
			return nil
		}
	case BooleanField:
		if _, ok := value.(*booleanExpression); ok {
			return nil
		}
	}
	return errors.New("type mismatched")
}
