-- Inventory storage table
CREATE TABLE IF NOT EXISTS inventory (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    CHECK (id = 1)
);

-- Sequence for VM inspection ordering
CREATE SEQUENCE IF NOT EXISTS vm_inspection_status_seq START 1;

CREATE TABLE IF NOT EXISTS vm_inspection_status (
    "VM ID" VARCHAR PRIMARY KEY,
    status VARCHAR NOT NULL,
    error VARCHAR,
    sequence INTEGER DEFAULT nextval('vm_inspection_status_seq'),
    details VARCHAR
);

CREATE SEQUENCE IF NOT EXISTS outbox_id_seq START 1;

CREATE TABLE IF NOT EXISTS outbox (
    id INTEGER PRIMARY KEY DEFAULT nextval('outbox_id_seq'),
    event_type VARCHAR NOT NULL,
    payload BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS groups (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    name VARCHAR NOT NULL UNIQUE,
    filter VARCHAR NOT NULL,
    description VARCHAR,
    inventory_data BLOB
);

CREATE TABLE IF NOT EXISTS group_matches (
    group_id UUID PRIMARY KEY,
    vm_ids VARCHAR[]
);

CREATE SEQUENCE IF NOT EXISTS vm_inspection_concerns_seq START 1;
CREATE SEQUENCE IF NOT EXISTS vm_inspection_id_seq START 1;

CREATE TABLE IF NOT EXISTS vm_inspection_concerns (
    id INTEGER PRIMARY KEY DEFAULT nextval('vm_inspection_concerns_seq'),
    "VM ID" VARCHAR NOT NULL,
    inspection_id INTEGER NOT NULL,
    category VARCHAR,
    label VARCHAR,
    msg VARCHAR,
    FOREIGN KEY ("VM ID") REFERENCES vinfo("VM ID")
);

CREATE TABLE IF NOT EXISTS rightsizing_reports (
    id                    VARCHAR      PRIMARY KEY,
    vcenter               VARCHAR      NOT NULL,
    cluster_id            VARCHAR      NOT NULL DEFAULT '',
    interval_id           INTEGER      NOT NULL,
    window_start          TIMESTAMPTZ  NOT NULL,
    window_end            TIMESTAMPTZ  NOT NULL,
    expected_sample_count INTEGER      NOT NULL,
    expected_batch_count  INTEGER      NOT NULL,
    written_batch_count   INTEGER      NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT current_timestamp
);

CREATE TABLE IF NOT EXISTS rightsizing_metrics (
    report_id    VARCHAR  NOT NULL,
    vm_name      VARCHAR  NOT NULL,
    moid         VARCHAR  NOT NULL,
    metric_key   VARCHAR  NOT NULL,
    sample_count INTEGER  NOT NULL,
    average      DOUBLE   NOT NULL,
    p95          DOUBLE   NOT NULL,
    p99          DOUBLE   NOT NULL,
    max          DOUBLE   NOT NULL,
    latest       DOUBLE   NOT NULL,
    PRIMARY KEY (report_id, moid, metric_key),
    FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id)
);

CREATE TABLE IF NOT EXISTS rightsizing_vm_warnings (
    report_id VARCHAR NOT NULL,
    moid      VARCHAR NOT NULL,
    vm_name   VARCHAR NOT NULL,
    warning   VARCHAR NOT NULL,
    PRIMARY KEY (report_id, moid),
    FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id)
);

CREATE TABLE IF NOT EXISTS rightsizing_vm_utilization (
    report_id             VARCHAR NOT NULL,
    moid                  VARCHAR NOT NULL,
    vm_name               VARCHAR NOT NULL,
    cpu_avg_pct           DOUBLE,
    cpu_p95_pct           DOUBLE,
    cpu_max_pct           DOUBLE,
    cpu_latest_pct        DOUBLE,
    mem_avg_pct           DOUBLE,
    mem_p95_pct           DOUBLE,
    mem_max_pct           DOUBLE,
    mem_latest_pct        DOUBLE,
    disk_pct              DOUBLE,
    confidence_pct        DOUBLE,
    cluster_id            VARCHAR,
    provisioned_cpus      INTEGER,
    provisioned_memory_mb INTEGER,
    provisioned_disk_kb   DOUBLE,
    cluster_name          VARCHAR,
    PRIMARY KEY (report_id, moid),
    FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id)
);

ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Backing Devices" VARCHAR DEFAULT '[]';

ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "migration_excluded" BOOLEAN DEFAULT FALSE;

ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "labels" VARCHAR DEFAULT '[]';

CREATE TABLE IF NOT EXISTS vm_applications (
    app_name VARCHAR NOT NULL,
    app_desc VARCHAR NOT NULL,
    vm_id    VARCHAR NOT NULL,
    vm_name  VARCHAR NOT NULL,
    PRIMARY KEY (app_name, vm_id)
);
