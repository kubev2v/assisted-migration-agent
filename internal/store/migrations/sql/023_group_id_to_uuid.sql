-- Migrate group IDs from INTEGER to UUID
-- Uses DuckDB gen_random_uuid() for UUID generation

-- Step 1: Create temporary tables with UUID schema
CREATE TABLE groups_new (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    name VARCHAR NOT NULL UNIQUE,
    filter VARCHAR NOT NULL,
    description VARCHAR,
    inventory_data BLOB
);

CREATE TABLE group_matches_new (
    group_id UUID PRIMARY KEY,
    vm_ids VARCHAR[]
);

-- Step 2: Migrate groups data - generate new UUID for each group
-- Note: This creates fresh UUIDs, not deterministic from old IDs
-- Groups will get new UUIDs in the console on next sync
INSERT INTO groups_new (id, created_at, updated_at, name, filter, description, inventory_data)
SELECT
    gen_random_uuid() AS new_id,
    created_at,
    updated_at,
    name,
    filter,
    description,
    inventory_data
FROM groups;

-- Step 3: Migrate group_matches with new UUID references
-- Join with groups_new to get the new UUIDs
INSERT INTO group_matches_new (group_id, vm_ids)
SELECT
    gn.id AS new_group_id,
    gm.vm_ids
FROM group_matches gm
JOIN groups g ON gm.group_id = g.id
JOIN groups_new gn ON g.name = gn.name;

-- Step 4: Drop old tables
DROP TABLE group_matches;
DROP TABLE groups;

-- Step 5: Rename new tables
ALTER TABLE groups_new RENAME TO groups;
ALTER TABLE group_matches_new RENAME TO group_matches;

-- Step 6: Drop the auto-increment sequence
DROP SEQUENCE IF EXISTS id_sequence;
