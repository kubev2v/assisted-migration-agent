-- Add backing device identifiers column to vdatastore for vendor/array derivation.
-- This column is populated by the migration-planner's IngestSqlite from forklift's
-- Datastore.BackingDevicesNames field. The migration handles the transition period
-- where the parser template may not yet include this column.
ALTER TABLE vdatastore ADD COLUMN IF NOT EXISTS "Backing Devices" VARCHAR DEFAULT '[]';
