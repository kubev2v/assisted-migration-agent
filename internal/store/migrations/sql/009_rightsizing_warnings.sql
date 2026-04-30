CREATE TABLE IF NOT EXISTS rightsizing_vm_warnings (
    report_id VARCHAR NOT NULL,
    moid      VARCHAR NOT NULL,
    vm_name   VARCHAR NOT NULL,
    warning   VARCHAR NOT NULL,
    PRIMARY KEY (report_id, moid),
    FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id)
);
