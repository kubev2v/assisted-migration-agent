-- VDDK status: single row (version, md5, path)
CREATE TABLE IF NOT EXISTS vddk (
    id INTEGER PRIMARY KEY DEFAULT 1,
    version VARCHAR,
    md5 VARCHAR,
    CHECK (id = 1)
);
