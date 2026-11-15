-- Existing collections need the storage array identity fields before datastore exports query them.
ALTER TABLE IF EXISTS vdatastore ADD COLUMN IF NOT EXISTS "Storage Arrays" VARCHAR;
ALTER TABLE IF EXISTS vdatastore ADD COLUMN IF NOT EXISTS "Storage Container ID" VARCHAR;
