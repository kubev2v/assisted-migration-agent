-- User-customizable VM metadata
CREATE TABLE IF NOT EXISTS vm_user_info (
    "VM ID" VARCHAR PRIMARY KEY,
    migration_excluded BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMP DEFAULT now(),
    FOREIGN KEY ("VM ID") REFERENCES vinfo("VM ID")
);
