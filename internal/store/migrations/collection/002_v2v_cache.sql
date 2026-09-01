-- Migration: V2V inspection tables and XML cache
-- This migration adds support for separate v2v inspection tracking alongside standard inspection.
-- Standard inspection uses vm_inspection_status/vm_inspection_concerns (from 001_initial.sql)
-- V2V inspection uses vm_inspection_status_v2v/vm_inspection_concerns_v2v (new tables below)

-- XML cache table for virt-inspector and virt-v2v-inspector outputs
-- Caches inspection results keyed by VM ID and snapshot ID to avoid re-running expensive inspections
CREATE TABLE IF NOT EXISTS vm_inspection_xmls (
    "VM ID" VARCHAR NOT NULL,
    snapshot_id VARCHAR NOT NULL,
    standard_xml TEXT,             -- JSON-serialized raw XML of standard virt-inspector
    v2v_xml TEXT,                  -- JSON-serialized raw XML of virt-v2v-inspector
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY ("VM ID", snapshot_id)
);

CREATE INDEX IF NOT EXISTS idx_vm_inspection_xmls_lookup
ON vm_inspection_xmls ("VM ID", snapshot_id);

-- V2V inspection status tracking (mirrors vm_inspection_status structure)
CREATE TABLE IF NOT EXISTS vm_inspection_status_v2v (
    "VM ID" VARCHAR PRIMARY KEY,
    status VARCHAR NOT NULL,       -- 'pending', 'running', 'completed', 'canceled', 'error'
    error TEXT,
    details TEXT,
    sequence INTEGER,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- V2V inspection concerns (mirrors vm_inspection_concerns structure)
CREATE SEQUENCE IF NOT EXISTS vm_inspection_id_v2v_seq START 1;

CREATE TABLE IF NOT EXISTS vm_inspection_concerns_v2v (
    id INTEGER PRIMARY KEY DEFAULT nextval('vm_inspection_concerns_seq'),
    "VM ID" VARCHAR NOT NULL,
    inspection_id INTEGER NOT NULL,
    category VARCHAR NOT NULL,     -- 'Info', 'Warning', 'Critical'
    label VARCHAR NOT NULL,
    msg TEXT NOT NULL,
    FOREIGN KEY ("VM ID") REFERENCES vinfo("VM ID")
);

CREATE INDEX IF NOT EXISTS idx_vm_inspection_concerns_v2v_vm_id
ON vm_inspection_concerns_v2v ("VM ID");
