-- Older collection databases may not have this table. Keep VM detail queries
-- valid and return empty optional storage metadata for those collections.
CREATE TABLE IF NOT EXISTS vhostscsi (
    "Host ID" VARCHAR,
    "HBA Device" VARCHAR,
    "SCSI Disk Key" VARCHAR,
    "Canonical Name" VARCHAR,
    "LUN Vendor" VARCHAR,
    "LUN Model" VARCHAR,
    "LUN UUID" VARCHAR
);

-- Keep VM detail queries compatible with collection databases created before
-- HBA protocol collection was added.
CREATE TABLE IF NOT EXISTS vhba (
    "Host ID" VARCHAR,
    "Device" VARCHAR,
    "Protocol" VARCHAR
);

ALTER TABLE vhba ADD COLUMN IF NOT EXISTS "Host ID" VARCHAR;
ALTER TABLE vhba ADD COLUMN IF NOT EXISTS "Device" VARCHAR;
ALTER TABLE vhba ADD COLUMN IF NOT EXISTS "Protocol" VARCHAR;

-- Preserve unknown RDM LUN identity in collections created before this field was ingested.
ALTER TABLE vdisk ADD COLUMN IF NOT EXISTS "RDM LUN UUID" VARCHAR;

-- Older collections keep unknown metadata NULL until it is collected.
ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Filesystem Type" VARCHAR;
ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Filesystem Major Version" INTEGER;
ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Storage Protocol" VARCHAR;
