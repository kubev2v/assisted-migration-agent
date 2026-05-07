-- Add labels column to vinfo table for user-defined VM labels
ALTER TABLE IF EXISTS vinfo ADD COLUMN IF NOT EXISTS "labels" VARCHAR DEFAULT '[]';
