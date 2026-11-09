-- Existing collections need these Planner-owned columns before host exports query them.
ALTER TABLE IF EXISTS vhost ADD COLUMN IF NOT EXISTS "CPU Model" VARCHAR;
ALTER TABLE IF EXISTS vhost ADD COLUMN IF NOT EXISTS "Serial Number" VARCHAR;
