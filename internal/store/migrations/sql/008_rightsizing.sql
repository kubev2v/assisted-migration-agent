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
