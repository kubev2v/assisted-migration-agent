-- Add prep_duration_sec to track disk creation + random fill time per benchmark run.
ALTER TABLE forecast_runs ADD COLUMN IF NOT EXISTS prep_duration_sec DOUBLE DEFAULT 0;
