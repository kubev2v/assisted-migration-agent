CREATE TABLE IF NOT EXISTS configuration (
    id INTEGER PRIMARY KEY DEFAULT 1,
    agent_mode VARCHAR DEFAULT 'disconnected',
    CHECK (id = 1)
);

-- VDDK status: single row (version, md5, path)
CREATE TABLE IF NOT EXISTS vddk (
    id INTEGER PRIMARY KEY DEFAULT 1,
    version VARCHAR,
    md5 VARCHAR,
    CHECK (id = 1)
);

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
    prep_duration_sec DOUBLE DEFAULT 0,
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

-- Add backing device identifiers column to vdatastore for vendor/array derivation.
-- This column is populated by the migration-planner's IngestSqlite from forklift's
-- Datastore.BackingDevicesNames field. The migration handles the transition period
-- where the parser template may not yet include this column.
-- TODO 
-- ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Backing Devices" VARCHAR DEFAULT '[]';

CREATE TABLE IF NOT EXISTS master_password (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    password VARCHAR NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id VARCHAR PRIMARY KEY,
    url VARCHAR NOT NULL,
    username VARCHAR NOT NULL,
    password VARCHAR NOT NULL,
    skip_tls BOOLEAN DEFAULT false,
    ca_cert VARCHAR DEFAULT ''
);

-- Create collections table: source of truth for lifecycle and publication state
CREATE TABLE IF NOT EXISTS collections (
    "database" VARCHAR PRIMARY KEY,
    state VARCHAR NOT NULL CHECK (state IN ('running',  'failed')),
    error VARCHAR
);
