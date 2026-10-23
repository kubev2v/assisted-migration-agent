DROP TABLE IF EXISTS application_definitions;
DROP TABLE IF EXISTS vm_vapp_products;
DROP TABLE IF EXISTS vm_vapp_products_tmp;

ALTER TABLE vm_applications DROP COLUMN IF EXISTS app_category;
ALTER TABLE vm_applications DROP COLUMN IF EXISTS app_vendor;
ALTER TABLE vm_applications DROP COLUMN IF EXISTS confidence;
