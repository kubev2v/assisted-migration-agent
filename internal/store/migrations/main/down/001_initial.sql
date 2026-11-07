-- Appliance recovery never downgrades below the initialized version 1 baseline.
DROP TABLE collections;
DROP TABLE credentials;
DROP TABLE master_password;
DROP TABLE datastore_capabilities;
DROP TABLE forecast_runs;
DROP SEQUENCE forecast_run_seq;
DROP SEQUENCE forecast_session_seq;
DROP TABLE vddk;
DROP TABLE configuration;
