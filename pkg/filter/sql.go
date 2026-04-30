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
	// vinfo (v) — string fields
	case "id":
		return `v."VM ID"`, StringField, nil
	case "name":
		return `v."VM"`, StringField, nil
	case "folder_id":
		return `v."Folder ID"`, StringField, nil
	case "folder":
		return `v."Folder"`, StringField, nil
	case "host":
		return `v."Host"`, StringField, nil
	case "smbios_uuid":
		return `v."SMBIOS UUID"`, StringField, nil
	case "vm_uuid":
		return `v."VM UUID"`, StringField, nil
	case "firmware":
		return `v."Firmware"`, StringField, nil
	case "powerstate", "status":
		return `v."Powerstate"`, StringField, nil
	case "connection_state":
		return `v."Connection state"`, StringField, nil
	case "ft_state":
		return `v."FT State"`, StringField, nil
	case "os_config":
		return `v."OS according to the configuration file"`, StringField, nil
	case "os_tools":
		return `v."OS according to the VMware Tools"`, StringField, nil
	case "dns_name":
		return `v."DNS Name"`, StringField, nil
	case "ip_address":
		return `v."Primary IP Address"`, StringField, nil
	case "hw_version":
		return `v."HW version"`, StringField, nil
	case "resource_pool":
		return `v."Resource pool"`, StringField, nil
	case "datacenter":
		return `v."Datacenter"`, StringField, nil
	case "cluster":
		return `v."Cluster"`, StringField, nil

	// vinfo (v) — numeric fields
	case "cpus":
		return `v."CPUs"`, NumericField, nil
	case "memory":
		return `v."Memory"`, NumericField, nil
	case "storage_used":
		return `v."In Use MiB"`, NumericField, nil
	case "total_disk_capacity":
		return `d.total_disk`, NumericField, nil
	case "provisioned":
		return `v."Provisioned MiB"`, NumericField, nil
	case "issues_count":
		return `cc."issues_count"`, NumericField, nil

	// vinfo (v) — boolean fields
	case "template":
		return `v."Template"`, BooleanField, nil
	case "cbt":
		return `v."CBT"`, BooleanField, nil
	case "enable_uuid":
		return `v."EnableUUID"`, BooleanField, nil
	case "migratable":
		return `(COALESCE(crit.critical_count, 0) = 0)`, BooleanField, nil
	case "migration_excluded":
		return `COALESCE(vui.migration_excluded, FALSE)`, BooleanField, nil

	// vdisk (dk) — disk.* prefix
	case "disk.path":
		return `dk."Disk Path"`, StringField, nil
	case "disk.sharing":
		return `dk."Sharing mode"`, StringField, nil
	case "disk.shared_bus":
		return `dk."Shared Bus"`, StringField, nil
	case "disk.mode":
		return `dk."Disk Mode"`, StringField, nil
	case "disk.controller":
		return `dk."Controller"`, StringField, nil
	case "disk.label":
		return `dk."Label"`, StringField, nil
	case "disk.key":
		return `dk."Disk Key"`, NumericField, nil
	case "disk.capacity":
		return `dk."Capacity MiB"`, NumericField, nil
	case "disk.raw":
		return `dk."Raw"`, BooleanField, nil
	case "disk.thin":
		return `dk."Thin"`, BooleanField, nil

	// concerns (c) — concern.* prefix
	case "concern.label":
		return `c."Label"`, StringField, nil
	case "concern.category":
		return `c."Category"`, StringField, nil
	case "concern.assessment":
		return `c."Assessment"`, StringField, nil

	// vm_inspection_status (i) — inspection.* prefix
	case "inspection.status":
		return `i.status`, StringField, nil
	case "inspection.error":
		return `i.error`, StringField, nil

	// vm_inspection_concerns (ic) — inspection_concern.* prefix
	case "inspection_concern.label":
		return `ic.label`, StringField, nil
	case "inspection_concern.category":
		return `ic.category`, StringField, nil
	case "inspection_concern.msg":
		return `ic.msg`, StringField, nil

	// vcpu (cpu) — cpu.* prefix
	case "cpu.sockets":
		return `cpu."Sockets"`, NumericField, nil
	case "cpu.cores_per_socket":
		return `cpu."Cores p/s"`, NumericField, nil
	case "cpu.hot_add":
		return `cpu."Hot Add"`, BooleanField, nil
	case "cpu.hot_remove":
		return `cpu."Hot Remove"`, BooleanField, nil

	// vmemory (mem) — mem.* prefix
	case "mem.ballooned":
		return `mem."Ballooned"`, NumericField, nil
	case "mem.hot_add":
		return `mem."Hot Add"`, BooleanField, nil

	// vnetwork (net) — net.* prefix
	case "net.network":
		return `net."Network"`, StringField, nil
	case "net.mac":
		return `net."Mac Address"`, StringField, nil
	case "net.nic_label":
		return `net."NIC label"`, StringField, nil
	case "net.adapter":
		return `net."Adapter"`, StringField, nil
	case "net.switch":
		return `net."Switch"`, StringField, nil
	case "net.type":
		return `net."Type"`, StringField, nil
	case "net.ipv4":
		return `net."IPv4 Address"`, StringField, nil
	case "net.ipv6":
		return `net."IPv6 Address"`, StringField, nil
	case "net.cluster":
		return `net."Cluster"`, StringField, nil
	case "net.connected":
		return `net."Connected"`, BooleanField, nil
	case "net.starts_connected":
		return `net."Starts Connected"`, BooleanField, nil

	// vdatastore (ds) — datastore.* prefix
	case "datastore.name":
		return `ds."Name"`, StringField, nil
	case "datastore.address":
		return `ds."Address"`, StringField, nil
	case "datastore.object_id":
		return `ds."Object ID"`, StringField, nil
	case "datastore.mha":
		return `ds."MHA"`, StringField, nil
	case "datastore.type":
		return `ds."Type"`, StringField, nil
	case "datastore.hosts":
		return `ds."Hosts"`, NumericField, nil
	case "datastore.free":
		return `ds."Free MiB"`, NumericField, nil
	case "datastore.capacity":
		return `ds."Capacity MiB"`, NumericField, nil

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
