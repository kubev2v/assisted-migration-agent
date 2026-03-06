package store

import sq "github.com/Masterminds/squirrel"

// vmOutputQuery is the base aggregated output query that produces one row per VM.
// Filters should be applied via Where clauses on the VM ID.
var vmOutputQuery = sq.Select(
	`v."VM ID" AS id`,
	`v."VM" AS name`,
	`v."Powerstate" AS power_state`,
	`COALESCE(v."Cluster", '') AS cluster`,
	`COALESCE(v."Datacenter", '') AS datacenter`,
	`v."Memory" AS memory`,
	`COALESCE(d.total_disk, 0) AS disk_size`,
	`COALESCE(c.issues_count, 0) AS issue_count`,
	`COALESCE(i.status, 'not_found') AS status`,
	`v."Template" as template`,
	`COALESCE(crit.critical_count, 0) = 0 AS migratable`,
	`COALESCE(i.error, '') AS error`,
).From("vinfo v").
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`)

// vmFilterSubquery is the base flat JOIN query for filtering.
// It joins all tables so WHERE clauses can reference any raw column.
// Filters should be applied via Where clauses, then use the result to get DISTINCT VM IDs.
var vmFilterSubquery = sq.Select(`DISTINCT v."VM ID"`).
	From("vinfo v").
	LeftJoin(`vdisk dk ON v."VM ID" = dk."VM ID"`).
	LeftJoin(`concerns c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`).
	LeftJoin(`vcpu cpu ON v."VM ID" = cpu."VM ID"`).
	LeftJoin(`vmemory mem ON v."VM ID" = mem."VM ID"`).
	LeftJoin(`vnetwork net ON v."VM ID" = net."VM ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") cc ON v."VM ID" = cc."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)`)
