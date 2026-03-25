-- Inspection concerns table: One global inspection id per save, all concern rows share it.

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
