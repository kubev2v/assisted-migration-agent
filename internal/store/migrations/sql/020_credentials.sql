CREATE TABLE master_password (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    password VARCHAR NOT NULL
);

CREATE TABLE credentials (
    id VARCHAR PRIMARY KEY,
    url VARCHAR NOT NULL,
    username VARCHAR NOT NULL,
    password VARCHAR NOT NULL
);
