CREATE TABLE IF NOT EXISTS rightsizing_vm_utilization (
    report_id      VARCHAR NOT NULL,
    moid           VARCHAR NOT NULL,
    vm_name        VARCHAR NOT NULL,
    cpu_avg_pct    DOUBLE,
    cpu_p95_pct    DOUBLE,
    cpu_max_pct    DOUBLE,
    cpu_latest_pct DOUBLE,
    mem_avg_pct    DOUBLE,
    mem_p95_pct    DOUBLE,
    mem_max_pct    DOUBLE,
    mem_latest_pct DOUBLE,
    disk_pct       DOUBLE,
    confidence_pct DOUBLE,
    PRIMARY KEY (report_id, moid),
    FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id)
);
