package store

// Export COPY queries build migration-planning CSV views, not raw table dumps.
// They join inventory with concerns, inspection, utilization, and groups and
// compute migration_status (Ready/Review/Blocked/Excluded) aligned with the VM API.

type exportScopeSpec struct {
	filename string
	query    string
}

// Shared SQL for overview and vms scopes: total disk per VM from vdisk.
const vmDiskTotalsJoin = `
			LEFT JOIN (
				SELECT "VM ID", SUM("Capacity MiB") AS total_disk_mib, COUNT(*) AS virtual_disk_count
				FROM vdisk
				GROUP BY "VM ID"
			) d ON v."VM ID" = d."VM ID"
`

// vmMigrationStatusExpr classifies each VM for migration planning (same rules as the VM list API).
const vmMigrationStatusExpr = `
				CASE
					WHEN v."migration_excluded" = TRUE THEN 'Excluded'
					WHEN NOT COALESCE(c_crit.critical_count > 0, FALSE) AND COALESCE(c.issues_count, 0) = 0 THEN 'Ready'
					WHEN NOT COALESCE(c_crit.critical_count > 0, FALSE) AND COALESCE(c.issues_count, 0) > 0 THEN 'Review'
					ELSE 'Blocked'
				END AS migration_status`

// vmAssessmentSelectCols adds migration readiness fields joined from concerns, inspection,
// latest rightsizing utilization, groups, and VM labels.
const vmAssessmentSelectCols = `
` + vmMigrationStatusExpr + `,
				COALESCE(c_issues.critical_issues, '') AS critical_issues,
				COALESCE(c.issues_count, 0) AS issue_count,
				COALESCE(i.status, 'not_started') AS inspection_status,
				COALESCE(ic.concern_count, 0) AS inspection_concern_count,
				COALESCE(u.cpu_p95, 0) AS cpu_p95,
				COALESCE(u.mem_p95, 0) AS mem_p95,
				COALESCE(u.utilization_confidence, 'none') AS utilization_confidence,
				COALESCE(grps.groups, '') AS groups,
				COALESCE(array_to_string(v."labels", '; '), '') AS labels`

// vmAssessmentJoins aggregates concerns, inspection status/concerns, utilization, and group membership per VM.
const vmAssessmentJoins = `
			LEFT JOIN (
				SELECT "VM_ID", COUNT(*) AS issues_count
				FROM concerns
				GROUP BY "VM_ID"
			) c ON v."VM ID" = c."VM_ID"

			LEFT JOIN (
				SELECT
					"VM_ID",
					COUNT(*) AS critical_count
				FROM concerns
				WHERE "Category" = 'Critical'
				GROUP BY "VM_ID"
			) c_crit ON v."VM ID" = c_crit."VM_ID"

			LEFT JOIN (
				SELECT
					"VM_ID",
					string_agg("Label", '; ') AS critical_issues
				FROM concerns
				WHERE "Category" IN ('Critical', 'Warning')
				GROUP BY "VM_ID"
			) c_issues ON v."VM ID" = c_issues."VM_ID"

			LEFT JOIN vm_inspection_status i ON v."VM ID" = i."VM ID"

			LEFT JOIN (
				SELECT "VM ID", COUNT(*) AS concern_count
				FROM vm_inspection_concerns ic
				WHERE ic.inspection_id = (
					SELECT MAX(inspection_id)
					FROM vm_inspection_concerns imx
					WHERE imx."VM ID" = ic."VM ID"
				)
				GROUP BY "VM ID"
			) ic ON v."VM ID" = ic."VM ID"

			LEFT JOIN (
				SELECT
					moid,
					cpu_p95_pct AS cpu_p95,
					mem_p95_pct AS mem_p95,
					CASE
						WHEN confidence_pct >= 80 THEN 'high'
						WHEN confidence_pct >= 50 THEN 'medium'
						WHEN confidence_pct > 0 THEN 'low'
						ELSE 'none'
					END AS utilization_confidence
				FROM rightsizing_vm_utilization
				WHERE report_id = (
					SELECT id FROM rightsizing_reports
					WHERE written_batch_count > 0
					ORDER BY created_at DESC LIMIT 1
				)
			) u ON v."VM ID" = u.moid

			LEFT JOIN (
				SELECT
					g.vm_id AS vm_id,
					string_agg(DISTINCT grp.name, '; ') AS groups
				FROM group_matches gm
				JOIN groups grp ON gm.group_id = grp.id
				, UNNEST(gm.vm_ids) AS g(vm_id)
				GROUP BY g.vm_id
			) grps ON v."VM ID" = grps.vm_id
`

// overviewQuery — scope "overview": one row per VM with core inventory and migration readiness summary.
// Joins vinfo, disk totals, and vmAssessment* fragments. Default export scope.
var overviewQuery = `

		COPY (
			SELECT
				v."VM" AS name,
				v."VM ID" AS id,
				v."Datacenter" AS datacenter,
				v."Cluster" AS cluster,
				v."Host" AS host,
				v."CPUs" AS cpu_count,
				v."Memory" AS memory_mib,
				ROUND(v."Memory" / 1024.0, 2) AS memory_gib,
				COALESCE(d.virtual_disk_count, 0) AS virtual_disk_count,
				COALESCE(d.total_disk_mib, 0) AS disk_mib,
				ROUND(COALESCE(d.total_disk_mib, 0) / 1024.0, 2) AS disk_gib,
				v."OS according to the VMware Tools" AS guest_os,

				-- Migration status (computed: Ready/Review/Blocked/Excluded)
` + vmAssessmentSelectCols + `

			FROM vinfo v
` + vmDiskTotalsJoin + `
` + vmAssessmentJoins + `

			ORDER BY v."VM"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// vmsQuery — scope "vms": full VM detail export including hardware, storage, networks, and assessment columns.
// Same assessment joins as overview; adds wide vinfo columns and up to eight network fields.
var vmsQuery = `

		COPY (
			SELECT
				v."VM" AS name,
				v."VM ID" AS id,
				v."Folder" AS folder,
				v."Template" AS is_template,
				v."Datacenter" AS datacenter,
				v."Cluster" AS cluster,
				v."Host" AS host,
				v."Resource pool" AS resource_pool,

				-- Hardware
				v."CPUs" AS cpu_count,
				v."Memory" AS memory_mib,
				ROUND(v."Memory" / 1024.0, 2) AS memory_gib,
				v."HW version" AS hardware_version,
				v."Firmware" AS firmware,

				-- Storage
				COALESCE(d.virtual_disk_count, 0) AS virtual_disk_count,
				COALESCE(d.total_disk_mib, 0) AS disk_capacity_mib,
				ROUND(COALESCE(d.total_disk_mib, 0) / 1024.0, 2) AS disk_capacity_gib,
				v."In Use MiB" AS disk_used_mib,
				ROUND(v."In Use MiB" / 1024.0, 2) AS disk_used_gib,
				v."Provisioned MiB" AS disk_provisioned_mib,
				ROUND(v."Provisioned MiB" / 1024.0, 2) AS disk_provisioned_gib,
				v."OsDiskComplexity" AS os_disk_complexity,

				-- State
				v."Powerstate" AS power_state,
				v."Connection state" AS connection_state,
				v."FT State" AS fault_tolerance_state,

				-- OS and Guest
				v."OS according to the configuration file" AS configured_os,
				v."OS according to the VMware Tools" AS guest_os,
				v."DNS Name" AS dns_name,
				v."Primary IP Address" AS primary_ip,

				-- Features
				v."CBT" AS change_block_tracking,
				v."EnableUUID" AS uuid_enabled,

				-- Identifiers
				v."SMBIOS UUID" AS smbios_uuid,
				v."VM UUID" AS vm_uuid,
				v."VI SDK UUID" AS vi_sdk_uuid,

				-- Networks
				COALESCE(v."NICs", 0) AS nic_count,
				COALESCE(v."Network #1", '') AS network_1,
				COALESCE(v."Network #2", '') AS network_2,
				COALESCE(v."Network #3", '') AS network_3,
				COALESCE(v."Network #4", '') AS network_4,
				COALESCE(v."Network #5", '') AS network_5,
				COALESCE(v."Network #6", '') AS network_6,
				COALESCE(v."Network #7", '') AS network_7,
				COALESCE(v."Network #8", '') AS network_8,

				-- Migration Assessment
				v."migration_excluded" AS migration_excluded,
` + vmAssessmentSelectCols + `,
				COALESCE(hn.nics, '[]') AS host_physical_nics,
				COALESCE(hv.interfaces, '[]') AS host_vmkernel_interfaces

			FROM vinfo v
			-- Aggregate NICs per host to keep one export row per VM.
			LEFT JOIN (
				SELECT
					"Host ID",
					to_json(LIST(
						CASE
							WHEN "Speed" IS NULL THEN json_object('id', "Network Device")
							ELSE json_object('id', "Network Device", 'linkSpeedMbps', "Speed")
						END
						ORDER BY "Network Device"
					)) AS nics
				FROM (` + hostPhysicalNICsQuery + `) n
				WHERE "Host ID" != ''
				GROUP BY "Host ID"
			) hn ON hn."Host ID" = v."Host ID"
			LEFT JOIN network_host_interfaces hv ON hv.host = v."Host"
` + vmDiskTotalsJoin + `
` + vmAssessmentJoins + `

			ORDER BY v."VM"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// hostsQuery — scope "hosts": ESXi host inventory with aggregated VM count per host.
const hostsQuery = `

		COPY (
			SELECT
				COALESCE(h."Object ID", '') AS id,
				COALESCE(h."Datacenter", '') AS datacenter,
				COALESCE(h."Cluster", '') AS cluster,
				h."# CPU" AS cpu_sockets,
				h."# Cores" AS cpu_cores,
				h."# Memory" AS memory_mib,
				ROUND(h."# Memory" / 1024.0, 2) AS memory_gib,
				COALESCE(h."Vendor", '') AS vendor,
				COALESCE(h."Model", '') AS model,
				COALESCE(h."Config status", '') AS config_status,
				COALESCE(vm_counts.vm_count, 0) AS vm_count,
				h."VMotion support" AS vmotion_supported,
				h."Storage VMotion support" AS storage_vmotion_supported,
				CASE WHEN LOWER(TRIM(h."VI SDK UUID")) = 'vm' THEN NULL
                    ELSE NULLIF(TRIM(h."VI SDK UUID"), '') END AS vcenter_id

			FROM vhost h

			-- VM count per host
			LEFT JOIN (
				SELECT "Host", COUNT(*) AS vm_count
				FROM vinfo
				GROUP BY "Host"
			) vm_counts ON h."Host" = vm_counts."Host"

			ORDER BY h."Cluster", h."Host"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// clustersQuery — scope "clusters": cluster settings plus host and VM counts per cluster.
const clustersQuery = `

		COPY (
			SELECT
				COALESCE(c."Name", '') AS name,
				COALESCE(c."Object ID", '') AS id,
				COALESCE(c."DrsEnabled", false) AS drs_enabled,
				COALESCE(c."DrsDefaultVmBehavior", 'None') AS drs_default_vm_behavior,
				COALESCE(host_counts.host_count, 0) AS host_count,
				COALESCE(vm_counts.vm_count, 0) AS vm_count

			FROM vcluster c

			-- Host count per cluster
			LEFT JOIN (
				SELECT "Cluster", COUNT(*) AS host_count
				FROM vhost
				GROUP BY "Cluster"
			) host_counts ON c."Name" = host_counts."Cluster"

			-- VM count per cluster
			LEFT JOIN (
				SELECT "Cluster", COUNT(*) AS vm_count
				FROM vinfo
				GROUP BY "Cluster"
			) vm_counts ON c."Name" = vm_counts."Cluster"

			ORDER BY c."Name"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

const datastoreProtocolExpr = `COALESCE(
    NULLIF(backing_info.protocols, ''),
    CASE
        WHEN LOWER(TRIM(ds."Storage Protocol")) IN ('fibrechannel', 'fibre channel', 'fc') THEN 'FibreChannel'
        ELSE NULLIF(TRIM(ds."Storage Protocol"), '')
    END
)`

// Keep these access categories aligned with inventory.StorageProtocolFromDatastoreType.
const datastoreAccessCategoryExpr = `CASE UPPER(TRIM(ds."Type"))
    WHEN 'VMFS' THEN 'Block'
    WHEN 'NFS' THEN 'File'
    WHEN 'NFS41' THEN 'File'
    WHEN 'NFS4.1' THEN 'File'
    WHEN 'NFS 4.1' THEN 'File'
    WHEN 'VSAN' THEN 'Object'
END`

const exportStorageTopologyQuery = `
            export_host_candidates AS (
                SELECT ds.datastore_row_id, TRIM(member.reference) AS reference, MIN(h.rowid) AS host_row_id
                FROM storage_datastores ds
                CROSS JOIN UNNEST(COALESCE(
                    TRY(from_json(ds."Hosts", '["VARCHAR"]')),
                    string_split(COALESCE(ds."Hosts", ''), ',')
                )) AS member(reference)
                JOIN vhost h ON TRIM(member.reference) = TRIM(h."Object ID")
                    OR LOWER(TRIM(member.reference)) = LOWER(TRIM(h."Host"))
                WHERE TRIM(member.reference) != ''
                GROUP BY ds.datastore_row_id, TRIM(member.reference)
                HAVING COUNT(DISTINCT h.rowid) = 1
            ),
            export_datastore_hosts AS (
                SELECT DISTINCT c.datastore_row_id, c.host_row_id, TRIM(h."Object ID") AS host_id
                FROM export_host_candidates c
                JOIN vhost h ON h.rowid = c.host_row_id
            ),
            export_storage_owners AS (
                SELECT TRIM("Object ID") AS host_id
                FROM vhost
                WHERE NULLIF(TRIM("Object ID"), '') IS NOT NULL
                GROUP BY TRIM("Object ID")
                HAVING COUNT(*) = 1
            ),
            export_storage_scsi AS (
                SELECT TRIM(s."Host ID") AS "Host ID",
                    s."HBA Device" AS "HBA Device",
                    s."Canonical Name" AS "Canonical Name",
                    s."LUN Vendor" AS "LUN Vendor",
                    s."LUN Model" AS "LUN Model"
                FROM vhostscsi s
                JOIN export_storage_owners owner ON owner.host_id = TRIM(s."Host ID")
            ),
            export_storage_hbas AS (
                SELECT TRIM(h."Host ID") AS "Host ID",
                    h."Device" AS "Device", h."Protocol" AS "Protocol"
                FROM vhba h
                JOIN export_storage_owners owner ON owner.host_id = TRIM(h."Host ID")
            ),
            export_disk_candidates AS (
                SELECT dk."VM ID", dk.rowid AS disk_row_id, ds.datastore_row_id,
                    member.host_row_id IS NOT NULL AS host_match
                FROM vdisk dk
                JOIN storage_datastores ds
                    ON ds."Name" = COALESCE(
                        NULLIF(TRIM(regexp_extract(TRIM(dk."Path"), '^\[([^\]]+)\]', 1)), ''),
                        NULLIF(TRIM(regexp_extract(TRIM(dk."Disk Path"), '^\[([^\]]+)\]', 1)), '')
                    )
                LEFT JOIN vinfo i ON i."VM ID" = dk."VM ID"
                LEFT JOIN storage_host_references vm_host ON vm_host.reference = TRIM(i."Host")
                LEFT JOIN export_datastore_hosts member
                    ON member.datastore_row_id = ds.datastore_row_id
                    AND member.host_row_id = vm_host.host_row_id
            ),
            export_disk_datastores AS (
                SELECT "VM ID", disk_row_id, datastore_row_id
                FROM export_disk_candidates
                QUALIFY COUNT(*) OVER (PARTITION BY "VM ID", disk_row_id) = 1
                    OR (host_match AND COUNT(*) FILTER (WHERE host_match)
                        OVER (PARTITION BY "VM ID", disk_row_id) = 1)
            )
`

const exportDatastoreBackingInfoQuery = `
vm_datastores AS (SELECT * FROM storage_datastores),
backing_datastore_hosts AS (SELECT * FROM export_datastore_hosts),
backing_scsi AS (SELECT * FROM export_storage_scsi),
backing_hbas AS (SELECT * FROM export_storage_hbas),
` + datastoreBackingInfoQuery

// datastoresQuery — scope "datastores": capacity, free space, used_percent, and VM count per datastore.
var datastoresQuery = `

		COPY (
			WITH {storage_topology},
            ` + exportStorageTopologyQuery + `,
            ` + exportDatastoreBackingInfoQuery + `
			SELECT
				COALESCE(ds."Name", '') AS name,
				COALESCE(ds."Object ID", '') AS id,
				COALESCE(ds."Address", '') AS address,
				COALESCE(ds."Type", '') AS type,
				ds."Capacity MiB" AS capacity_mib,
				ROUND(ds."Capacity MiB" / 1024.0, 2) AS capacity_gib,
				ds."Free MiB" AS free_mib,
				ROUND(ds."Free MiB" / 1024.0, 2) AS free_gib,
				CASE
					WHEN ds."Capacity MiB" > 0
					THEN ROUND((ds."Capacity MiB" - ds."Free MiB") / ds."Capacity MiB" * 100, 2)
					ELSE 0
				END AS used_percent,
				COALESCE(ds."MHA", false) AS mha_enabled,
				COALESCE(ds."Hosts", '') AS hosts,
				COALESCE(vm_counts.vm_count, 0) AS vm_count,
                (ds."Capacity MiB" - ds."Free MiB") / 1024.0 AS used_gib,
                ds."Storage Protocol" AS storage_protocol,
                ds."Filesystem Type" AS filesystem_type,
                ds."Filesystem Major Version" AS filesystem_major_version,
                ds."Backing Devices" AS backing_devices,
                ds."SIOC Enabled" AS sioc_enabled,
                ` + datastoreProtocolExpr + ` AS protocol_type,
                ` + datastoreAccessCategoryExpr + ` AS storage_access_category,
                backing_info.vendor AS vendor,
                backing_info.model AS model

			FROM storage_datastores ds
            LEFT JOIN backing_info ON backing_info.datastore_row_id = ds.datastore_row_id

			LEFT JOIN (
				SELECT
					datastore_row_id,
					COUNT(DISTINCT "VM ID") AS vm_count
				FROM export_disk_datastores
				GROUP BY datastore_row_id
			) vm_counts ON ds.datastore_row_id = vm_counts.datastore_row_id

			ORDER BY ds."Name"
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// hostStorageOwners resolves only host IDs that identify a single inventory row.
const hostStorageOwners = `
    SELECT TRIM("Object ID") AS host_id, MIN("Host") AS host
    FROM vhost
    WHERE NULLIF(TRIM("Object ID"), '') IS NOT NULL
    GROUP BY TRIM("Object ID")
    HAVING COUNT(*) = 1
`

const hbasQuery = `
    COPY (
        SELECT h."Host ID" AS host_id, owners.host AS host, h."Device" AS device,
            h."Type" AS adapter_type, h."Protocol" AS protocol,
            h."Model" AS model, h."Driver" AS driver,
            h."Node WWN" AS node_wwn, h."Port WWN" AS port_wwn,
            h."HBA Key" AS adapter_key
        FROM vhba h
        LEFT JOIN (` + hostStorageOwners + `) owners ON TRIM(h."Host ID") = owners.host_id
        ORDER BY host_id, device, adapter_key
    ) TO ? (FORMAT CSV, HEADER TRUE)
`

const lunsQuery = `
    COPY (
        SELECT s."Host ID" AS host_id, owners.host AS host,
            s."HBA Device" AS hba_device, s."Canonical Name" AS canonical_name,
            s."LUN Vendor" AS lun_vendor, s."LUN Model" AS lun_model,
            s."LUN UUID" AS lun_uuid, s."SCSI Disk Key" AS scsi_disk_key
        FROM vhostscsi s
        LEFT JOIN (` + hostStorageOwners + `) owners ON TRIM(s."Host ID") = owners.host_id
        ORDER BY host_id, hba_device, canonical_name, scsi_disk_key
    ) TO ? (FORMAT CSV, HEADER TRUE)
`

// networkQuery — scope "network": one row per NIC from vnetwork with stable nic_index ordering per VM.
const networkQuery = `

		COPY (
			SELECT
				COALESCE(v."VM", '') AS vm_name,
				COALESCE(n."VM ID", '') AS vm_id,
				COALESCE(n."Cluster", '') AS cluster,
				ROW_NUMBER() OVER (
					PARTITION BY n."VM ID"
					ORDER BY
						-- Extract numeric suffix for proper ordering (e.g., "Network adapter 2" < "Network adapter 10")
						TRY_CAST(regexp_extract(n."NIC label", '\d+$', 0) AS INTEGER),
						n."NIC label"
				) - 1 AS nic_index,
				COALESCE(v."NICs", 0) AS nic_count,
				COALESCE(n."Network", '') AS network,
				ng.port_group AS port_group,
				COALESCE(n."Switch", '') AS switch,
				COALESCE(n."Type", '') AS type,
				COALESCE(n."NIC label", '') AS nic_label,
				COALESCE(n."Adapter", '') AS adapter,
				COALESCE(n."Mac Address", '') AS mac_address,
				COALESCE(array_to_string(n."IPv4 Addresses", ','), '') AS ipv4_address,
				COALESCE(array_to_string(n."IPv6 Addresses", ','), '') AS ipv6_address,
				COALESCE(n."VLAN", '') AS vlan_id,
				COALESCE(n."Connected", false) AS connected,
				COALESCE(n."Starts Connected", false) AS starts_connected

			FROM vnetwork n
			LEFT JOIN network_adapter_links ng ON ng.nic_row_id = n.rowid

			-- Join with vinfo to get VM name
			LEFT JOIN vinfo v ON n."VM ID" = v."VM ID"

			ORDER BY n."Cluster", v."VM", nic_index
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// disksQuery — scope "disks": one row per stored virtual disk with its VM identity.
const disksQuery = `
		COPY (
            WITH {storage_topology},
            ` + exportStorageTopologyQuery + `,
            ` + exportDatastoreBackingInfoQuery + `
			SELECT
				COALESCE(v."VM", '') AS vm_name,
				d."VM ID" AS vm_id,
				COALESCE(v."Datacenter", '') AS datacenter,
				COALESCE(v."Cluster", '') AS cluster,
				COALESCE(v."Host", '') AS host,
				COALESCE(d."Disk Key", '') AS disk_key,
				COALESCE(d."Unit #", '') AS unit_number,
				COALESCE(d."Path", '') AS path,
				COALESCE(d."Disk Path", '') AS disk_path,
				COALESCE(d."Capacity MiB", 0) AS capacity_mib,
				COALESCE(d."Sharing mode", false) AS sharing_mode,
				COALESCE(d."Raw", false) AS raw,
				COALESCE(d."Shared Bus", '') AS shared_bus,
				COALESCE(d."Disk Mode", '') AS disk_mode,
				COALESCE(d."Disk UUID", '') AS disk_uuid,
				COALESCE(d."Thin", false) AS thin,
				COALESCE(d."Controller", '') AS controller,
				COALESCE(d."Label", '') AS label,
				COALESCE(d."SCSI Unit #", '') AS scsi_unit_number,
                CASE WHEN d."Raw" THEN d."RDM LUN UUID" END AS rdm_lun_uuid,
                ds."Object ID" AS datastore_id,
                ds."Name" AS datastore_name,
                ds."Type" AS datastore_type,
                ds."Filesystem Type" AS datastore_filesystem_type,
                ds."Filesystem Major Version" AS datastore_filesystem_major_version,
                ` + datastoreProtocolExpr + ` AS datastore_protocol_type,
                ` + datastoreAccessCategoryExpr + ` AS datastore_storage_protocol,
                ds."Backing Devices" AS datastore_backing_devices,
                backing_info.vendor AS datastore_vendor,
                backing_info.model AS datastore_model
			FROM vdisk d
			LEFT JOIN vinfo v ON d."VM ID" = v."VM ID"
            LEFT JOIN export_disk_datastores resolved ON resolved.disk_row_id = d.rowid AND resolved."VM ID" = d."VM ID"
            LEFT JOIN storage_datastores ds ON ds.datastore_row_id = resolved.datastore_row_id
            LEFT JOIN backing_info ON backing_info.datastore_row_id = ds.datastore_row_id
			ORDER BY v."VM", d."VM ID", TRY_CAST(d."Disk Key" AS BIGINT), d."Disk Key",
				TRY_CAST(d."Unit #" AS BIGINT), d."Unit #", d."Path", d."Disk Path"
		) TO ? (FORMAT CSV, HEADER TRUE)
`

// applicationsQuery — scope "applications": detected applications rolled up with VM count and id list.
const applicationsQuery = `

		COPY (
			SELECT
				COALESCE(app_name, '') AS application_name,
				COALESCE(MAX(app_desc), '') AS application_description,
				COALESCE(MAX(app_vendor), 'Unknown') AS application_vendor,
				COALESCE(MAX(app_category), 'Unclassified') AS application_category,
				COUNT(*) AS vm_count,
				COALESCE(string_agg(vm_id, '; ' ORDER BY vm_name), '') AS vm_ids

			FROM vm_applications

			GROUP BY app_name

			ORDER BY app_name
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// processesQuery — scope "processes": one row per guest process reported for a VM.
const processesQuery = `
		COPY (
			SELECT
				COALESCE(p.vm_name, '') AS vm_name,
				p.vm_id AS vm_id,
				COALESCE(p.cluster, '') AS cluster,
				json_extract_string(p.app, '$.name') AS process_name
			FROM (
				SELECT
					v."VM" AS vm_name,
					v."VM ID" AS vm_id,
					v."Cluster" AS cluster,
					unnest(json_extract(COALESCE(v."guest_apps", '[]'), '$[*]')) AS app
				FROM vinfo v
			) p
			WHERE COALESCE(json_extract_string(p.app, '$.name'), '') <> ''
			ORDER BY p.vm_name, p.vm_id, process_name
		) TO ? (FORMAT CSV, HEADER TRUE)
`

// groupsQuery — scope "groups": user-defined VM groups with metadata and matched VM count.
const groupsQuery = `

		COPY (
			SELECT
				COALESCE(g.id::VARCHAR, '') AS group_id,
				COALESCE(g.name, '') AS group_name,
				COALESCE(g.description, '') AS description,
				COALESCE(g.created_at::VARCHAR, '') AS created_at,
				COALESCE(g.updated_at::VARCHAR, '') AS updated_at,
				COALESCE(array_length(gm.vm_ids, 1), 0) AS vm_count

			FROM groups g

			LEFT JOIN group_matches gm ON g.id = gm.group_id

			ORDER BY g.name
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// inspectionQuery — scope "inspection": concern lines from the latest deep-inspection run per VM.
const inspectionQuery = `

		COPY (
			SELECT
				COALESCE(v."VM", '') AS vm_name,
				COALESCE(ic."VM ID", '') AS vm_id,
				COALESCE(ic.category, '') AS category,
				COALESCE(ic.label, '') AS label,
				COALESCE(ic.msg, '') AS message

			FROM vm_inspection_concerns ic

			-- Get VM name
			LEFT JOIN vinfo v ON ic."VM ID" = v."VM ID"

			-- Only latest inspection per VM
			WHERE ic.inspection_id = (
				SELECT MAX(inspection_id)
				FROM vm_inspection_concerns imx
				WHERE imx."VM ID" = ic."VM ID"
			)

			ORDER BY v."VM", ic.category, ic.label
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// storageForecastQuery — scope "storage-forecast": raw storage migration benchmark runs from forecast_runs.
const storageForecastQuery = `

		COPY (
			SELECT
				id,
				session_id,
				COALESCE(pair_name, '') AS pair_name,
				COALESCE(source_datastore, '') AS source_datastore,
				COALESCE(target_datastore, '') AS target_datastore,
				iteration,
				disk_size_gb,
				COALESCE(duration_sec, 0) AS duration_sec,
				COALESCE(throughput_mbps, 0) AS throughput_mbps,
				COALESCE(prep_duration_sec, 0) AS prep_duration_sec,
				COALESCE(method, '') AS method,
				COALESCE(error, '') AS error,
				COALESCE(created_at::VARCHAR, '') AS created_at

			FROM agent.main.forecast_runs

			ORDER BY session_id, pair_name, iteration
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// vmUtilizationCopyQueryTmpl — scope "utilization" (vm_utilization.csv): per-VM metrics from the latest
// completed rightsizing report. %s is a DuckDB string literal for report_id (see duckDBStringLiteral).
const vmUtilizationCopyQueryTmpl = `

		COPY (
			SELECT
				COALESCE(u.vm_name, '') AS vm_name,
				COALESCE(u.moid, '') AS vm_id,
				COALESCE(u.cluster_name, '') AS cluster,
				COALESCE(u.cpu_avg_pct, 0) AS cpu_avg_pct,
				COALESCE(u.cpu_p95_pct, 0) AS cpu_p95_pct,
				COALESCE(u.cpu_max_pct, 0) AS cpu_max_pct,
				COALESCE(u.cpu_latest_pct, 0) AS cpu_latest_pct,
				COALESCE(u.mem_avg_pct, 0) AS mem_avg_pct,
				COALESCE(u.mem_p95_pct, 0) AS mem_p95_pct,
				COALESCE(u.mem_max_pct, 0) AS mem_max_pct,
				COALESCE(u.mem_latest_pct, 0) AS mem_latest_pct,
				COALESCE(u.disk_pct, 0) AS disk_pct,
				COALESCE(u.confidence_pct, 0) AS confidence_pct,
				COALESCE(u.provisioned_cpus, 0) AS provisioned_cpus,
				COALESCE(u.provisioned_memory_mb, 0) AS provisioned_memory_mb,
				COALESCE(u.provisioned_disk_kb, 0) AS provisioned_disk_kb,
				COALESCE(r.created_at::VARCHAR, '') AS report_timestamp

			FROM rightsizing_vm_utilization u

			JOIN rightsizing_reports r ON u.report_id = r.id

			WHERE u.report_id = %s

			ORDER BY u.cluster_name, u.vm_name
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// clusterUtilizationCopyQueryTmpl — scope "utilization" (cluster_utilization.csv): cluster rollups with
// provisioned-resource-weighted CPU/memory/disk averages from the same report as vmUtilizationCopyQueryTmpl.
const clusterUtilizationCopyQueryTmpl = `

		COPY (
			SELECT
				COALESCE(u.cluster_name, '') AS cluster,
				COUNT(*) AS vm_count,
				COALESCE(
					SUM(u.cpu_avg_pct * u.provisioned_cpus) / NULLIF(SUM(u.provisioned_cpus), 0),
					0
				) AS cpu_avg_pct,
				COALESCE(
					SUM(u.cpu_p95_pct * u.provisioned_cpus) / NULLIF(SUM(u.provisioned_cpus), 0),
					0
				) AS cpu_p95_pct,
				COALESCE(
					SUM(u.cpu_max_pct * u.provisioned_cpus) / NULLIF(SUM(u.provisioned_cpus), 0),
					0
				) AS cpu_max_pct,
				COALESCE(
					SUM(u.mem_avg_pct * u.provisioned_memory_mb) / NULLIF(SUM(u.provisioned_memory_mb), 0),
					0
				) AS mem_avg_pct,
				COALESCE(
					SUM(u.mem_p95_pct * u.provisioned_memory_mb) / NULLIF(SUM(u.provisioned_memory_mb), 0),
					0
				) AS mem_p95_pct,
				COALESCE(
					SUM(u.mem_max_pct * u.provisioned_memory_mb) / NULLIF(SUM(u.provisioned_memory_mb), 0),
					0
				) AS mem_max_pct,
				COALESCE(
					SUM(u.disk_pct * u.provisioned_disk_kb) / NULLIF(SUM(u.provisioned_disk_kb), 0),
					0
				) AS disk_pct,
				COALESCE(AVG(u.confidence_pct), 0) AS confidence_pct,
				COALESCE(SUM(u.provisioned_cpus), 0) AS total_provisioned_cpus,
				COALESCE(SUM(u.provisioned_memory_mb), 0) AS total_provisioned_memory_mb,
				COALESCE(SUM(u.provisioned_disk_kb), 0) AS total_provisioned_disk_kb,
				COALESCE(MAX(r.created_at)::VARCHAR, '') AS report_timestamp

			FROM rightsizing_vm_utilization u

			JOIN rightsizing_reports r ON u.report_id = r.id

			WHERE u.report_id = %s

			GROUP BY u.cluster_name

			ORDER BY u.cluster_name
		) TO ? (FORMAT CSV, HEADER TRUE)
	
`

// latestRightsizingReportIDQuery selects the most recent rightsizing report with written batches.
const latestRightsizingReportIDQuery = `
		SELECT id
		FROM rightsizing_reports
		WHERE written_batch_count > 0
		ORDER BY created_at DESC
		LIMIT 1
	`

var exportScopes = map[string]exportScopeSpec{
	"overview":         {filename: "overview.csv", query: overviewQuery},
	"hosts":            {filename: "hosts.csv", query: hostsQuery},
	"clusters":         {filename: "clusters.csv", query: clustersQuery},
	"datastores":       {filename: "datastores.csv", query: datastoresQuery},
	"hbas":             {filename: "hbas.csv", query: hbasQuery},
	"luns":             {filename: "luns.csv", query: lunsQuery},
	"vms":              {filename: "vms.csv", query: vmsQuery},
	"network":          {filename: "networks.csv", query: networkQuery},
	"disks":            {filename: "disks.csv", query: disksQuery},
	"applications":     {filename: "applications.csv", query: applicationsQuery},
	"processes":        {filename: "processes.csv", query: processesQuery},
	"groups":           {filename: "groups.csv", query: groupsQuery},
	"inspection":       {filename: "inspection.csv", query: inspectionQuery},
	"storage-forecast": {filename: "storage-forecast.csv", query: storageForecastQuery},
}
