ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "Eagerly Scrubbed";
ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "Thin Provisioned";
ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "RDM LUN UUID Source";
ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "RDM Compatibility Mode";
ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "RDM Backing UUID";
ALTER TABLE IF EXISTS vdisk DROP COLUMN IF EXISTS "RDM Device Name";
-- vinfo "Host ID" and "VI SDK UUID" predate this migration in collections that
-- already carried them; dropping them could destroy collected data, so they stay.
