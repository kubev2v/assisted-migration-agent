CREATE TABLE IF NOT EXISTS configuration (
    id INTEGER PRIMARY KEY DEFAULT 1,
    agent_mode VARCHAR DEFAULT 'disconnected',
    CHECK (id = 1)
);

-- Inventory storage table
CREATE TABLE IF NOT EXISTS inventory (
    id INTEGER PRIMARY KEY DEFAULT 1,
    data BLOB NOT NULL,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    CHECK (id = 1)
);

-- VM Inspection status table
CREATE TABLE IF NOT EXISTS vm_inspection (
    vm_moid VARCHAR PRIMARY KEY,
    status VARCHAR NOT NULL,
    error VARCHAR
--     FOREIGN KEY (vm_moid) REFERENCES vinfo("VM ID") -- we can add this in case vinfo is created before
);
