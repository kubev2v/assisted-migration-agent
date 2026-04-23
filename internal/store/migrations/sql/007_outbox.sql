CREATE SEQUENCE outbox_id_seq START 1;

CREATE TABLE IF NOT EXISTS outbox (
    id INTEGER PRIMARY KEY DEFAULT nextval('outbox_id_seq'),
    event_type VARCHAR NOT NULL,
    payload BLOB NOT NULL
);

