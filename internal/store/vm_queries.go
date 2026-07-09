package store

import sq "github.com/Masterminds/squirrel"

// vmGetQuery fetches a single VM by ID with full details (disks, NICs, concerns)
// plus utilization data from the latest rightsizing report.
// Mirrors the vendored duckdb_parser vm_query template but adds the utilization JOIN.
const vmGetQuery = `
WITH disks AS (
    SELECT
        dk."VM ID",
        LIST({
            'Key': dk."Disk Key",
            'UnitNumber': dk."Unit #",
            'File': COALESCE(dk."Path", dk."Disk Path"),
            'Capacity': COALESCE(dk."Capacity MiB", 0),
            'Shared': dk."Sharing mode",
            'RDM': dk."Raw",
            'Bus': dk."Shared Bus",
            'Mode': dk."Disk Mode",
            'Serial': dk."Disk UUID",
            'Thin': dk."Thin",
            'Controller': dk."Controller",
            'Label': dk."Label",
            'SCSIUnit': dk."SCSI Unit #",
            'Datastore': COALESCE(ds."Object ID", '')
        }) AS disks
    FROM vdisk dk
    LEFT JOIN vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)
    GROUP BY dk."VM ID"
),
nics AS (
    SELECT
        "VM ID",
        LIST({
            'Network': "Network",
            'MAC': "Mac Address",
            'Label': "NIC label",
            'Adapter': "Adapter",
            'Switch': "Switch",
            'Connected': "Connected",
            'StartsConnected': "Starts Connected",
            'Type': "Type",
            'IPv4Address': CASE WHEN "IPv4 Address" = 'VM' OR "IPv4 Address" IS NULL THEN '' ELSE "IPv4 Address" END,
            'IPv6Address': CASE WHEN "IPv6 Address" = 'VM' OR "IPv6 Address" IS NULL THEN '' ELSE "IPv6 Address" END
        }) AS nics
    FROM vnetwork
    GROUP BY "VM ID"
),
concerns_agg AS (
    SELECT
        "VM_ID",
        LIST({
            'Id': "Concern_ID",
            'Label': "Label",
            'Category': "Category",
            'Assessment': "Assessment"
        }) AS concerns
    FROM concerns
    GROUP BY "VM_ID"
),
groups_agg AS (
    SELECT
        u.vm_id,
        ARRAY_AGG(DISTINCT grp.name) AS groups
    FROM group_matches gm
    JOIN groups grp ON gm.group_id = grp.id
    , UNNEST(gm.vm_ids) AS u(vm_id)
    GROUP BY u.vm_id
)
SELECT
    COALESCE(i."VM ID", '') AS "ID",
    COALESCE(i."VM", '') AS "Name",
    COALESCE(i."Folder ID", i."Folder", '') AS "Folder",
    COALESCE(i."Host", '') AS "Host",
    COALESCE(i."SMBIOS UUID", i."VM UUID", '') AS "UUID",
    COALESCE(i."Firmware", '') AS "Firmware",
    COALESCE(i."Powerstate", '') AS "PowerState",
    COALESCE(i."Connection state", '') AS "ConnectionState",
    (i."FT State" IS NOT NULL AND i."FT State" != '' AND i."FT State" != 'notConfigured') AS "FaultToleranceEnabled",
    COALESCE(i."CPUs", 0) AS "CpuCount",
    COALESCE(i."Memory", 0) AS "MemoryMB",
    COALESCE(i."OS according to the configuration file", '') AS "GuestName",
    COALESCE(i."OS according to the VMware Tools", '') AS "GuestNameFromVmwareTools",
    COALESCE(i."DNS Name", '') AS "HostName",
    COALESCE(i."Primary IP Address", '') AS "IpAddress",
    CAST(COALESCE(i."In Use MiB", 0) AS BIGINT) * 1024 * 1024 AS "StorageUsed",
    COALESCE(i."Template", false) AS "IsTemplate",
    COALESCE(i."CBT", false) AS "ChangeTrackingEnabled",
    COALESCE(i."EnableUUID", false) AS "DiskEnableUuid",
    COALESCE(i."Datacenter", '') AS "Datacenter",
    COALESCE(i."Cluster", '') AS "Cluster",
    COALESCE(i."HW version", '') AS "HWVersion",
    COALESCE(i."Total disk capacity MiB", 0) AS "TotalDiskCapacityMiB",
    COALESCE(i."Provisioned MiB", 0) AS "ProvisionedMiB",
    COALESCE(i."Resource pool", '') AS "ResourcePool",
    COALESCE(i."OsDiskComplexity", 0) AS "OsDiskComplexity",
    COALESCE(i."migration_excluded", false) AS "MigrationExcluded",
    COALESCE(CAST(i."labels" AS VARCHAR[]), [])::VARCHAR[] AS "Labels",
    COALESCE(g.groups, [])::VARCHAR[] AS "Groups",
    COALESCE(c."Hot Add", false) AS "CpuHotAddEnabled",
    COALESCE(c."Hot Remove", false) AS "CpuHotRemoveEnabled",
    COALESCE(c."Sockets", 0) AS "CpuSockets",
    COALESCE(c."Cores p/s", 0) AS "CoresPerSocket",
    COALESCE(m."Hot Add", false) AS "MemoryHotAddEnabled",
    COALESCE(m."Ballooned", 0) AS "BalloonedMemory",
    COALESCE(d.disks, []) AS "Disks",
    COALESCE(n.nics, []) AS "NICs",
    LIST_FILTER(
        LIST_VALUE(i."Network #1", i."Network #2", i."Network #3", i."Network #4",
                   i."Network #5", i."Network #6", i."Network #7", i."Network #8"),
        x -> x IS NOT NULL AND x != '' AND x != 'VM'
    ) AS "Networks",
    COALESCE(con.concerns, []) AS "Concerns",
    u.moid,
    u.vm_name,
    u.provisioned_cpus,
    u.provisioned_memory_mb,
    u.provisioned_disk_kb,
    u.cpu_avg_pct,
    u.cpu_p95_pct,
    u.cpu_max_pct,
    u.cpu_latest_pct,
    u.mem_avg_pct,
    u.mem_p95_pct,
    u.mem_max_pct,
    u.mem_latest_pct,
    u.disk_pct,
    u.confidence_pct,
    COALESCE(i."guest_apps", '[]') AS "GuestApps"
FROM vinfo i
LEFT JOIN vcpu c ON i."VM ID" = c."VM ID"
LEFT JOIN vmemory m ON i."VM ID" = m."VM ID"
LEFT JOIN disks d ON i."VM ID" = d."VM ID"
LEFT JOIN nics n ON i."VM ID" = n."VM ID"
LEFT JOIN concerns_agg con ON i."VM ID" = con."VM_ID"
LEFT JOIN groups_agg g ON i."VM ID" = g.vm_id
LEFT JOIN rightsizing_vm_utilization u
    ON u.moid = i."VM ID"
    AND u.report_id = (
        SELECT id FROM rightsizing_reports
        WHERE written_batch_count > 0
        ORDER BY created_at DESC LIMIT 1
    )
WHERE i."VM ID" = ?;
`

// vmFilterOptionsQuery returns distinct clusters, datacenters, concern labels,
// and concern categories in a single row.
const vmFilterOptionsQuery = `
SELECT
    (SELECT COALESCE(list(DISTINCT "Cluster" ORDER BY "Cluster"), [])
     FROM vinfo WHERE "Cluster" IS NOT NULL AND "Cluster" != '') AS clusters,
    (SELECT COALESCE(list(DISTINCT "Datacenter" ORDER BY "Datacenter"), [])
     FROM vinfo WHERE "Datacenter" IS NOT NULL AND "Datacenter" != '') AS datacenters,
    (SELECT COALESCE(list(DISTINCT "Label" ORDER BY "Label"), [])
     FROM concerns WHERE "Label" IS NOT NULL AND "Label" != '') AS concern_labels,
    (SELECT COALESCE(list(DISTINCT "Category" ORDER BY "Category"), [])
     FROM concerns WHERE "Category" IS NOT NULL AND "Category" != '') AS concern_categories,
    (SELECT COALESCE(list(DISTINCT app_name ORDER BY app_name), [])
     FROM vm_applications) AS applications
`

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
	`COALESCE(i.status, 'not_started') AS status`,
	`COALESCE(i.details, '') AS details`,
	`v."Template" as template`,
	`COALESCE(crit.critical_count, 0) = 0 AS migratable`,
	`COALESCE(i.error, '') AS error`,
	`COALESCE((SELECT COUNT(*)::BIGINT FROM vm_inspection_concerns ic WHERE ic."VM ID" = v."VM ID" AND ic.inspection_id = (SELECT MAX(inspection_id) FROM vm_inspection_concerns imx WHERE imx."VM ID" = v."VM ID")), 0) AS inspection_concern_count`,
	`COALESCE(g.groups, [])::VARCHAR[] AS groups`,
	`v."migration_excluded" AS migration_excluded`,
	`COALESCE(CAST(v."labels" AS VARCHAR[]), [])::VARCHAR[] AS labels`,
).From("vinfo v").
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`).
	LeftJoin(`(
		SELECT u.vm_id, ARRAY_AGG(DISTINCT grp.name) AS groups
		FROM group_matches gm
		JOIN groups grp ON gm.group_id = grp.id
		, UNNEST(gm.vm_ids) AS u(vm_id)
		GROUP BY u.vm_id
	) g ON v."VM ID" = g.vm_id`)

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
	LeftJoin(`vdatastore ds ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)`).
	LeftJoin(`vm_inspection_concerns ic ON v."VM ID" = ic."VM ID" AND ic.inspection_id = (SELECT MAX(inspection_id) FROM vm_inspection_concerns imx WHERE imx."VM ID" = v."VM ID")`).
	LeftJoin(`(
SELECT moid, vm_name,
       provisioned_cpus, provisioned_memory_mb, provisioned_disk_kb,
       cpu_avg_pct, cpu_p95_pct, cpu_max_pct, cpu_latest_pct,
       mem_avg_pct, mem_p95_pct, mem_max_pct, mem_latest_pct,
       disk_pct, confidence_pct
FROM rightsizing_vm_utilization
WHERE report_id = (
      SELECT id FROM rightsizing_reports
      WHERE written_batch_count > 0
      ORDER BY created_at DESC LIMIT 1
  )) as utilization ON v."VM ID" = utilization.moid`).
	LeftJoin(`vm_applications va ON v."VM ID" = va.vm_id`).
	LeftJoin(`(
		SELECT u.vm_id, ARRAY_AGG(DISTINCT grp.name) AS groups
		FROM group_matches gm
		JOIN groups grp ON gm.group_id = grp.id
		, UNNEST(gm.vm_ids) AS u(vm_id)
		GROUP BY u.vm_id
	) g ON v."VM ID" = g.vm_id`)
