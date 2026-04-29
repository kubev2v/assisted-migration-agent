-- Add migration_excluded column to vinfo table
-- Note: This migration may be a no-op if the schema was created by migration-planner
-- library v0.12+ which includes this column in the default schema
ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "migration_excluded" BOOLEAN DEFAULT FALSE;
