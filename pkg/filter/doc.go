// Package filter provides a DSL for filtering VMs using SQL-like expressions.
//
// The filter package implements a lexer, parser, and SQL generator that converts
// human-readable filter expressions into squirrel Sqlizer objects for use with
// SelectBuilder queries.
//
// # Grammar
//
//	expression  : term ( "or" term )* ;
//	term        : factor ( "and" factor )* ;
//	factor      : equality | "(" expression ")" ;
//	equality    : IDENTIFIER ( "=" | "!=" | "<" | "<=" | ">" | ">=" ) value
//	            | IDENTIFIER ( "~" | "!~" ) REGEX_LITERAL
//	            | IDENTIFIER "in" "[" STRING ( "," STRING )* "]"
//	            | IDENTIFIER "not" "in" "[" STRING ( "," STRING )* "]" ;
//	value       : STRING | QUANTITY | BOOLEAN ;
//
//	IDENTIFIER    : [a-zA-Z_][a-zA-Z0-9_.]* ;
//	REGEX_LITERAL : '/' ( '\\/' | . )*? '/' ;
//	STRING        : "'" (.*?) "'" | '"' (.*?) '"' ;
//	BOOLEAN       : "true" | "false" ;
//	QUANTITY      : [0-9]+(\.[0-9]+)? ( 'KB' | 'MB' | 'GB' | 'TB' )? ;
//
// # Operators
//
//	=    Equal
//	!=   Not equal
//	>    Greater than
//	>=   Greater than or equal
//	<    Less than
//	<=   Less than or equal
//	~      Regex match (uses regexp_matches)
//	!~     Regex not match
//	in     Membership test (SQL IN clause)
//	not in Exclusion test (SQL NOT IN clause)
//	and    Logical AND (higher precedence than OR)
//	or     Logical OR
//
// # Value Types
//
// Strings: Single or double quoted. Empty strings are allowed.
// Use backslash to escape the enclosing quote character: \' inside '…', \" inside "…".
//
//	name = 'production'
//	name = "test-vm"
//	name = 'it\'s a test'
//	description = ''
//
// Booleans: Case-insensitive true/false.
//
//	active = true
//	enabled = FALSE
//
// Quantities: Numbers with optional size units (KB, MB, GB, TB).
// All quantities are normalized to MB for comparison.
//
//	memory > 8GB        // 8192 MB
//	disk >= 1TB         // 1048576 MB
//	memory < 512MB      // 512 MB
//	memory > 1024KB     // 1 MB
//	count = 100         // plain number (no conversion)
//
// Regex: AWK-style patterns between forward slashes.
//
//	name ~ /^prod-.*/           // starts with "prod-"
//	name ~ /web|api/            // contains "web" or "api"
//	name !~ /test/              // does not contain "test"
//	path ~ /a\/b/               // escaped slash matches "a/b"
//
// Lists: Comma-separated strings in square brackets for IN/NOT IN operators.
//
//	status in ['active', 'pending', 'running']
//	cluster in ['prod', 'staging']
//	status not in ['deleted', 'archived']
//	name not in ['test-vm', 'dev-vm']
//
// # Identifiers
//
// Identifiers support dotted notation for nested fields:
//
//	vm.name = 'test'
//	vm.host.datacenter = 'DC1'
//	config.nested.value > 100
//
// # Operator Precedence
//
// AND binds tighter than OR. Use parentheses to override:
//
//	a = '1' or b = '2' and c = '3'       // a OR (b AND c)
//	(a = '1' or b = '2') and c = '3'     // (a OR b) AND c
//
// # Usage with squirrel SelectBuilder
//
// The Parse function returns a squirrel.Sqlizer that can be used with
// SelectBuilder.Where():
//
//	import (
//	    sq "github.com/Masterminds/squirrel"
//	    "github.com/kubev2v/assisted-migration-agent/pkg/filter"
//	)
//
//	// Define a mapper from filter field names to SQL columns
//	mapper := filter.MapFunc(func(name string) (string, error) {
//	    switch name {
//	    case "name":
//	        return `v."VM"`, nil
//	    case "memory":
//	        return `v."Memory"`, nil
//	    case "cluster":
//	        return `v."Cluster"`, nil
//	    case "status":
//	        return `v."Powerstate"`, nil
//	    default:
//	        return "", fmt.Errorf("unknown field: %s", name)
//	    }
//	})
//
//	// Parse the filter expression
//	sqlizer, err := filter.Parse([]byte("memory > 8GB and status = 'poweredOn'"), mapper)
//	if err != nil {
//	    return err
//	}
//
//	// Use with SelectBuilder
//	query, args, err := sq.Select("*").
//	    From("vms").
//	    Where(sqlizer).
//	    ToSql()
//	// query: SELECT * FROM vms WHERE ((v."Memory" > ?) AND (v."Powerstate" = ?))
//	// args: [8192.00, "poweredOn"]
//
// IN operator generates SQL IN clauses:
//
//	sqlizer, _ := filter.Parse([]byte("status in ['poweredOn', 'suspended']"), mapper)
//	query, args, _ := sq.Select("*").From("vms").Where(sqlizer).ToSql()
//	// query: SELECT * FROM vms WHERE v."Powerstate" IN (?,?)
//	// args: ["poweredOn", "suspended"]
//
// # Default Field Mapping
//
// ParseWithDefaultMap uses a built-in MapFunc that maps identifiers to SQL
// column references in the flat filter subquery (see internal/store VMStore).
// Flat names reference vinfo columns; dotted names reference joined tables:
//
// vinfo (v) — flat names:
//
//	id, name, folder_id, folder, host, smbios_uuid, vm_uuid, firmware,
//	powerstate (alias: status), connection_state, ft_state, cpus, memory,
//	os_config, os_tools, dns_name, ip_address, storage_used, template,
//	cbt, enable_uuid, datacenter, cluster, hw_version, total_disk_capacity,
//	provisioned, resource_pool
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
// # Group Field Mapping
//
// ParseWithGroupMap uses a group-specific MapFunc that maps identifiers to
// columns in the groups table:
//
//	name        → "name"
//	description → "description"
//	filter      → "filter"
//
// Usage:
//
//	sqlizer, err := filter.ParseWithGroupMap([]byte("name = 'production'"))
//	groups, err := store.Group().List(ctx, []sq.Sqlizer{sqlizer}, 20, 0)
//
// # Usage with store
//
// ByFilter parses a DSL expression and returns a sq.Sqlizer for the flat
// filter subquery:
//
//	filters := []sq.Sqlizer{store.ByFilter("memory > 8GB and disk.capacity >= 100GB")}
//	vms, err := store.VM().List(ctx, filters, store.WithLimit(50))
//
// # Filter Examples
//
// Simple comparisons:
//
//	name = 'web-server-01'
//	status != 'poweredOff'
//	memory >= 16GB
//	template = false
//
// Cross-table filtering with dot notation:
//
//	disk.capacity >= 100GB
//	concern.category = 'Critical'
//	cpu.cores_per_socket >= 4
//	net.type = 'VmxNet3'
//	datastore.type = 'NFS'
//
// Regex matching:
//
//	name ~ /^prod-/
//	disk.path ~ /datastore1/
//	concern.label ~ /RDM/
//
// IN/NOT IN:
//
//	cluster in ['prod', 'staging']
//	concern.category not in ['Information']
//
// Combined filters:
//
//	memory >= 8GB and disk.capacity >= 100GB
//	(cluster = 'prod' or cluster = 'staging') and concern.category != 'Critical'
//	cpu.sockets >= 2 and mem.hot_add = true and net.connected = true
//
// # Error Handling
//
// Parse returns a ParseError with position information on syntax errors:
//
//	_, err := filter.Parse([]byte("name ="), mapper)
//	// err: parse error at 6: expected value instead of eol
//
// The MapFunc returns errors for unknown fields:
//
//	_, err := filter.ParseWithDefaultMap([]byte("unknown_field = 'x'"))
//	// err: unknown filter field: unknown_field
package filter
