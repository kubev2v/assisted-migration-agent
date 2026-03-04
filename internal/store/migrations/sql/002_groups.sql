CREATE SEQUENCE id_sequence START 1;

CREATE TABLE IF NOT EXISTS groups (
    id INTEGER PRIMARY KEY DEFAULT nextval('id_sequence'),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),
    name VARCHAR NOT NULL UNIQUE,
    filter VARCHAR NOT NULL,
    description VARCHAR
);

