-- Forecast tables for migration time estimation between datastore pairs.

CREATE SEQUENCE IF NOT EXISTS forecast_session_seq START 1;
CREATE SEQUENCE IF NOT EXISTS forecast_run_seq START 1;

CREATE TABLE IF NOT EXISTS forecast_runs (
    id INTEGER PRIMARY KEY DEFAULT nextval('forecast_run_seq'),
    session_id INTEGER NOT NULL,
    pair_name VARCHAR NOT NULL,
    source_datastore VARCHAR NOT NULL,
    target_datastore VARCHAR NOT NULL,
    iteration INTEGER NOT NULL,
    disk_size_gb INTEGER NOT NULL,
    duration_sec DOUBLE NOT NULL,
    throughput_mbps DOUBLE NOT NULL,
    method VARCHAR,
    error VARCHAR,
    created_at TIMESTAMP DEFAULT now()
);

CREATE TABLE IF NOT EXISTS datastore_capabilities (
    datastore_name VARCHAR PRIMARY KEY,
    datastore_type VARCHAR,
    storage_vendor VARCHAR,
    storage_model VARCHAR,
    xcopy_supported BOOLEAN DEFAULT FALSE,
    rdm_feasible BOOLEAN DEFAULT FALSE,
    vvol_feasible BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMP DEFAULT now()
);
