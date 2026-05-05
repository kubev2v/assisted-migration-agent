CREATE TABLE IF NOT EXISTS vm_filter (
    -- vinfo (v_)
    v_vm_id VARCHAR,
    v_vm VARCHAR,
    v_folder_id VARCHAR,
    v_folder VARCHAR,
    v_host VARCHAR,
    v_smbios_uuid VARCHAR,
    v_vm_uuid VARCHAR,
    v_firmware VARCHAR,
    v_powerstate VARCHAR,
    v_connection_state VARCHAR,
    v_ft_state VARCHAR,
    v_os_config VARCHAR,
    v_os_tools VARCHAR,
    v_dns_name VARCHAR,
    v_ip_address VARCHAR,
    v_hw_version VARCHAR,
    v_resource_pool VARCHAR,
    v_datacenter VARCHAR,
    v_cluster VARCHAR,
    v_cpus BIGINT,
    v_memory BIGINT,
    v_in_use_mib BIGINT,
    v_provisioned_mib BIGINT,
    v_template BOOLEAN,
    v_cbt BOOLEAN,
    v_enable_uuid BOOLEAN,

    -- vdisk (dk_)
    dk_disk_path VARCHAR,
    dk_sharing_mode VARCHAR,
    dk_shared_bus VARCHAR,
    dk_disk_mode VARCHAR,
    dk_controller VARCHAR,
    dk_label VARCHAR,
    dk_disk_key BIGINT,
    dk_capacity_mib BIGINT,
    dk_raw BOOLEAN,
    dk_thin BOOLEAN,

    -- concerns (c_)
    c_label VARCHAR,
    c_category VARCHAR,
    c_assessment VARCHAR,

    -- vm_inspection_status (inspection_)
    inspection_status VARCHAR,
    inspection_error VARCHAR,

    -- vcpu (cpu_)
    cpu_sockets BIGINT,
    cpu_cores_ps BIGINT,
    cpu_hot_add BOOLEAN,
    cpu_hot_remove BOOLEAN,

    -- vmemory (mem_)
    mem_ballooned BIGINT,
    mem_hot_add BOOLEAN,

    -- vnetwork (net_)
    net_network VARCHAR,
    net_mac_address VARCHAR,
    net_nic_label VARCHAR,
    net_adapter VARCHAR,
    net_switch VARCHAR,
    net_type VARCHAR,
    net_ipv4_address VARCHAR,
    net_ipv6_address VARCHAR,
    net_cluster VARCHAR,
    net_connected BOOLEAN,
    net_starts_connected BOOLEAN,

    -- vdatastore (ds_)
    ds_name VARCHAR,
    ds_address VARCHAR,
    ds_object_id VARCHAR,
    ds_mha BOOLEAN,
    ds_type VARCHAR,
    ds_hosts VARCHAR,
    ds_free_mib BIGINT,
    ds_capacity_mib BIGINT,

    -- vm_inspection_concerns (ic_)
    ic_label VARCHAR,
    ic_category VARCHAR,
    ic_msg VARCHAR,

    -- computed aggregates
    issues_count BIGINT,
    critical_count BIGINT,
    total_disk BIGINT,
    migratable BOOLEAN,

    -- utilization (u_)
    u_provisioned_cpus BIGINT,
    u_provisioned_memory_mb BIGINT,
    u_provisioned_disk_kb DOUBLE,
    u_cpu_avg_pct DOUBLE,
    u_cpu_p95_pct DOUBLE,
    u_cpu_max_pct DOUBLE,
    u_cpu_latest_pct DOUBLE,
    u_mem_avg_pct DOUBLE,
    u_mem_p95_pct DOUBLE,
    u_mem_max_pct DOUBLE,
    u_mem_latest_pct DOUBLE,
    u_disk_pct DOUBLE,
    u_confidence_pct DOUBLE
);
