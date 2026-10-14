package store

import (
	sq "github.com/Masterminds/squirrel"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
)

// vmGetQuery fetches a single VM by ID with full details (disks, NICs, concerns)
// plus utilization data from the latest rightsizing report.
// filtered_vm is resolved first to scope all subsequent CTEs to one VM,
// preventing full-table aggregations.
const vmGetQuery = `
filtered_vm AS (
    SELECT * FROM vinfo WHERE "VM ID" = ?
),
metadata_values AS (
    SELECT metadata.key, list(metadata.value ORDER BY metadata.value) AS values
    FROM vm_metadata metadata
    JOIN filtered_vm fv ON metadata.vm_id = fv."VM ID"
    GROUP BY metadata.key
),
metadata_agg AS (
    SELECT map(list(key ORDER BY key), list(values ORDER BY key)) AS entries
    FROM metadata_values
),
-- Datastores referenced by this VM's disks.
vm_datastores AS (
    SELECT ds.*
    FROM storage_datastores ds
    WHERE EXISTS (
        SELECT 1
        FROM storage_disk_datastores resolved
        JOIN filtered_vm fv ON resolved."VM ID" = fv."VM ID"
        WHERE resolved.datastore_row_id = ds.datastore_row_id
    )
),
-- Resolve each datastore backing device's transport and vendor/model through datastore hosts, SCSI LUNs, and HBAs.
backing_paths AS (
    SELECT
        ds.datastore_row_id,
        CASE
            WHEN LOWER(TRIM(hba."Protocol")) IN ('fibrechannel', 'fibre channel', 'fc') THEN 'FibreChannel'
            ELSE NULLIF(TRIM(hba."Protocol"), '')
        END AS protocol,
        NULLIF(TRIM(scsi."LUN Vendor"), '') AS vendor,
        NULLIF(TRIM(scsi."LUN Model"), '') AS model
    FROM vm_datastores ds
    CROSS JOIN UNNEST(COALESCE(
        TRY(from_json(NULLIF(ds."Backing Devices", ''), '["VARCHAR"]')),
        []::VARCHAR[]
    )) AS backing(device)
    JOIN storage_datastore_hosts member_host
        ON member_host.datastore_row_id = ds.datastore_row_id
    LEFT JOIN storage_scsi scsi
        ON scsi."Host ID" = member_host.host_id
        AND LOWER(TRIM(scsi."Canonical Name")) = LOWER(TRIM(backing.device))
    LEFT JOIN storage_hbas hba
        ON hba."Host ID" = scsi."Host ID"
        AND hba."Device" = scsi."HBA Device"
),
-- Aggregate distinct protocols, vendors, and models per datastore, excluding unknown protocols.
backing_info AS (
    SELECT
        datastore_row_id,
        string_agg(DISTINCT protocol, ', ' ORDER BY protocol)
            FILTER (WHERE protocol IS NOT NULL AND LOWER(TRIM(protocol)) != 'unknown') AS protocols,
        string_agg(
            DISTINCT vendor, ', ' ORDER BY vendor
        ) FILTER (WHERE vendor IS NOT NULL) AS vendor,
        string_agg(
            DISTINCT model, ', ' ORDER BY model
        ) FILTER (WHERE model IS NOT NULL) AS model
    FROM backing_paths
    GROUP BY datastore_row_id
),
disks AS (
    SELECT
        dk."VM ID",
        LIST({
            'Key': dk."Disk Key",
            'UnitNumber': dk."Unit #",
            'File': COALESCE(dk."Path", dk."Disk Path"),
            'Capacity': COALESCE(dk."Capacity MiB", 0),
            'Shared': dk."Sharing mode",
            'RDM': dk."Raw",
            -- Row JSON keeps older read-only collections without this column queryable.
            'RDMLunUUID': to_json(dk)->>'RDM LUN UUID',
            'Bus': dk."Shared Bus",
            'Mode': dk."Disk Mode",
            'Serial': dk."Disk UUID",
            'Thin': dk."Thin",
            'Controller': dk."Controller",
            'Label': dk."Label",
            'SCSIUnit': dk."SCSI Unit #",
            'Datastore': COALESCE(ds."Object ID", ''),
            'DatastoreName': COALESCE(ds."Name", ''),
            'DatastoreType': COALESCE(ds."Type", ''),
            'DatastoreFilesystemType': COALESCE(to_json(ds)->>'Filesystem Type', ''),
            'DatastoreFilesystemMajorVersion': COALESCE(TRY_CAST(to_json(ds)->>'Filesystem Major Version' AS INTEGER), 0),
            'DatastoreStorageProtocol': COALESCE(
                NULLIF(backing_info.protocols, ''),
                CASE
                    WHEN LOWER(TRIM(to_json(ds)->>'Storage Protocol')) IN ('fibrechannel', 'fibre channel', 'fc') THEN 'FibreChannel'
                    ELSE NULLIF(TRIM(to_json(ds)->>'Storage Protocol'), '')
                END,
                ''
            ),
            'DatastoreBackingDeviceIDs': COALESCE(
                TRY(from_json(NULLIF(ds."Backing Devices", ''), '["VARCHAR"]')),
                []::VARCHAR[]
            ),
            'DatastoreVendor': COALESCE(backing_info.vendor, ''),
            'DatastoreModel': COALESCE(backing_info.model, '')
        }) AS disks
    FROM vdisk dk
    JOIN filtered_vm fv ON dk."VM ID" = fv."VM ID"
    LEFT JOIN storage_disk_datastores resolved
        ON resolved.disk_row_id = dk.rowid AND resolved."VM ID" = dk."VM ID"
    LEFT JOIN storage_datastores ds ON ds.datastore_row_id = resolved.datastore_row_id
    LEFT JOIN backing_info ON backing_info.datastore_row_id = ds.datastore_row_id
    GROUP BY dk."VM ID"
),
nics AS (
    SELECT
        vnetwork."VM ID",
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
        } ORDER BY "Mac Address" ASC) AS nics
    FROM vnetwork
    JOIN filtered_vm fv ON vnetwork."VM ID" = fv."VM ID"
    GROUP BY vnetwork."VM ID"
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
    JOIN filtered_vm fv ON concerns."VM_ID" = fv."VM ID"
    GROUP BY "VM_ID"
),
groups_agg AS (
    SELECT
        u.vm_id,
        ARRAY_AGG(DISTINCT grp.name) AS groups
    FROM group_matches gm
    JOIN groups grp ON gm.group_id = grp.id
    , UNNEST(gm.vm_ids) AS u(vm_id)
    JOIN filtered_vm fv ON u.vm_id = fv."VM ID"
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
    COALESCE(i."FT State" IN ('enabled', 'running', 'starting', 'needSecondary', 'primary', 'secondary'), false) AS "FaultToleranceEnabled",
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
    COALESCE(i."guest_apps", '[]') AS "GuestApps",
    COALESCE(metadata.entries, map()) AS "Metadata",
    COALESCE(ins.status, 'not_started') AS "InspectionState",
    COALESCE(ins.details, '') AS "InspectionDetails",
    COALESCE(ins.error, '') AS "InspectionError"
FROM filtered_vm i
CROSS JOIN metadata_agg metadata
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
LEFT JOIN vm_inspection_status ins ON i."VM ID" = ins."VM ID"

`

// vmFilterOptionsQuery returns distinct values available for VM filters.
const vmFilterOptionsQuery = `
WITH metadata_values AS (
    SELECT key, list(DISTINCT value ORDER BY value) AS values
    FROM vm_metadata
    GROUP BY key
)
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
     FROM vm_applications) AS applications,
    (SELECT COALESCE(map(list(key ORDER BY key), list(values ORDER BY key)), map())
     FROM metadata_values) AS metadata
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
	`COALESCE(v."Template", false) AS template`,
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
// Filters should be applied via Where clauses, then use the result to get DISTINCT VM IDs.
var vmFilterSubquery = sq.Select(`DISTINCT v."VM ID"`).
	From("vinfo v").
	LeftJoin(`vm_metadata metadata ON metadata.vm_id = v."VM ID"`).
	LeftJoin(`vcluster vc ON vc."Name" = v."Cluster"`).
	LeftJoin(`vdisk dk ON v."VM ID" = dk."VM ID"`).
	LeftJoin(`concerns c ON v."VM ID" = c."VM_ID"`).
	LeftJoin(`vm_inspection_status i ON v."VM ID" = i."VM ID"`).
	LeftJoin(`vcpu cpu ON v."VM ID" = cpu."VM ID"`).
	LeftJoin(`vmemory mem ON v."VM ID" = mem."VM ID"`).
	LeftJoin(`vnetwork net ON v."VM ID" = net."VM ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS issues_count FROM concerns GROUP BY "VM_ID") cc ON v."VM ID" = cc."VM_ID"`).
	LeftJoin(`(SELECT "VM_ID", COUNT(*) AS critical_count FROM concerns WHERE "Category" = 'Critical' GROUP BY "VM_ID") crit ON v."VM ID" = crit."VM_ID"`).
	LeftJoin(`(SELECT "VM ID", SUM("Capacity MiB") AS total_disk FROM vdisk GROUP BY "VM ID") d ON v."VM ID" = d."VM ID"`).
	LeftJoin(`(WITH ` + duckdb_parser.DatastoreResolutionQuery() + `
		SELECT ds.*, resolved.disk_row_id, resolved."VM ID" AS resolved_vm_id
		FROM storage_disk_datastores resolved
		JOIN storage_datastores ds ON ds.datastore_row_id = resolved.datastore_row_id
	) ds ON ds.disk_row_id = dk.rowid AND ds.resolved_vm_id = v."VM ID"`).
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
