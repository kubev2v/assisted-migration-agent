ALTER TABLE vm_applications
ADD COLUMN IF NOT EXISTS app_vendor VARCHAR;

ALTER TABLE vm_applications
ADD COLUMN IF NOT EXISTS app_category VARCHAR;
