package filter

import (
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
)

// MapFunc resolves a filter identifier (e.g. "memory") to a fully qualified
// SQL column reference (e.g. m."Size MB"). The function should return an
// error for unknown identifiers.
type MapFunc func(name string) (string, error)

var defaultMapFn MapFunc = func(name string) (string, error) {
	switch strings.ToLower(name) {
	// vinfo (v)
	case "id":
		return `v."VM ID"`, nil
	case "name":
		return `v."VM"`, nil
	case "folder_id":
		return `v."Folder ID"`, nil
	case "folder":
		return `v."Folder"`, nil
	case "host":
		return `v."Host"`, nil
	case "smbios_uuid":
		return `v."SMBIOS UUID"`, nil
	case "vm_uuid":
		return `v."VM UUID"`, nil
	case "firmware":
		return `v."Firmware"`, nil
	case "powerstate", "status":
		return `v."Powerstate"`, nil
	case "connection_state":
		return `v."Connection state"`, nil
	case "ft_state":
		return `v."FT State"`, nil
	case "cpus":
		return `v."CPUs"`, nil
	case "memory":
		return `v."Memory"`, nil
	case "os_config":
		return `v."OS according to the configuration file"`, nil
	case "os_tools":
		return `v."OS according to the VMware Tools"`, nil
	case "dns_name":
		return `v."DNS Name"`, nil
	case "ip_address":
		return `v."Primary IP Address"`, nil
	case "storage_used":
		return `v."In Use MiB"`, nil
	case "template":
		return `v."Template"`, nil
	case "cbt":
		return `v."CBT"`, nil
	case "enable_uuid":
		return `v."EnableUUID"`, nil
	case "datacenter":
		return `v."Datacenter"`, nil
	case "cluster":
		return `v."Cluster"`, nil
	case "hw_version":
		return `v."HW version"`, nil
	case "total_disk_capacity":
		return `d.total_disk`, nil
	case "provisioned":
		return `v."Provisioned MiB"`, nil
	case "resource_pool":
		return `v."Resource pool"`, nil

	// issues count
	case "issues_count":
		return `cc."issues_count"`, nil
	case "migratable":
		return `(COALESCE(crit.critical_count, 0) = 0)`, nil

	// vdisk (dk) — disk.* prefix
	case "disk.key":
		return `dk."Disk Key"`, nil
	case "disk.path":
		return `dk."Disk Path"`, nil
	case "disk.capacity":
		return `dk."Capacity MiB"`, nil
	case "disk.sharing":
		return `dk."Sharing mode"`, nil
	case "disk.raw":
		return `dk."Raw"`, nil
	case "disk.shared_bus":
		return `dk."Shared Bus"`, nil
	case "disk.mode":
		return `dk."Disk Mode"`, nil
	case "disk.thin":
		return `dk."Thin"`, nil
	case "disk.controller":
		return `dk."Controller"`, nil
	case "disk.label":
		return `dk."Label"`, nil

	// concerns (c) — concern.* prefix
	case "concern.label":
		return `c."Label"`, nil
	case "concern.category":
		return `c."Category"`, nil
	case "concern.assessment":
		return `c."Assessment"`, nil

	// vm_inspection_status (i) — inspection.* prefix
	case "inspection.status":
		return `i.status`, nil
	case "inspection.error":
		return `i.error`, nil

	// vm_inspection_concerns (ic) — inspection_concern.* prefix (latest persisted inspection run per VM)
	case "inspection_concern.label":
		return `ic.label`, nil
	case "inspection_concern.category":
		return `ic.category`, nil
	case "inspection_concern.msg":
		return `ic.msg`, nil

	// vcpu (cpu) — cpu.* prefix
	case "cpu.hot_add":
		return `cpu."Hot Add"`, nil
	case "cpu.hot_remove":
		return `cpu."Hot Remove"`, nil
	case "cpu.sockets":
		return `cpu."Sockets"`, nil
	case "cpu.cores_per_socket":
		return `cpu."Cores p/s"`, nil

	// vmemory (mem) — mem.* prefix
	case "mem.hot_add":
		return `mem."Hot Add"`, nil
	case "mem.ballooned":
		return `mem."Ballooned"`, nil

	// vnetwork (net) — net.* prefix
	case "net.network":
		return `net."Network"`, nil
	case "net.mac":
		return `net."Mac Address"`, nil
	case "net.nic_label":
		return `net."NIC label"`, nil
	case "net.adapter":
		return `net."Adapter"`, nil
	case "net.switch":
		return `net."Switch"`, nil
	case "net.connected":
		return `net."Connected"`, nil
	case "net.starts_connected":
		return `net."Starts Connected"`, nil
	case "net.type":
		return `net."Type"`, nil
	case "net.ipv4":
		return `net."IPv4 Address"`, nil
	case "net.ipv6":
		return `net."IPv6 Address"`, nil
	case "net.cluster":
		return `net."Cluster"`, nil

	// vdatastore (ds) — datastore.* prefix
	case "datastore.name":
		return `ds."Name"`, nil
	case "datastore.hosts":
		return `ds."Hosts"`, nil
	case "datastore.address":
		return `ds."Address"`, nil
	case "datastore.object_id":
		return `ds."Object ID"`, nil
	case "datastore.free":
		return `ds."Free MiB"`, nil
	case "datastore.mha":
		return `ds."MHA"`, nil
	case "datastore.capacity":
		return `ds."Capacity MiB"`, nil
	case "datastore.type":
		return `ds."Type"`, nil

	default:
		return "", fmt.Errorf("unknown filter field: %s", name)
	}
}

var groupMapFn MapFunc = func(name string) (string, error) {
	switch strings.ToLower(name) {
	case "name":
		return "name", nil
	case "description":
		return "description", nil
	case "filter":
		return "filter", nil
	default:
		return "", fmt.Errorf("unknown group filter field: %s", name)
	}
}

func toSql(expr Expression, mf MapFunc) (sq.Sqlizer, error) {
	switch e := expr.(type) {
	case *binaryExpression:
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
		col, err := mf(strings.ToLower(e.Name))
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
		col, err := mf(strings.ToLower(e.Left.(*varExpression).Name))
		if err != nil {
			return nil, err
		}
		if e.Negated {
			return sq.NotEq{col: e.Values}, nil
		}
		return sq.Eq{col: e.Values}, nil
	default:
		return nil, fmt.Errorf("unknown expression type: %T", expr)
	}
}
