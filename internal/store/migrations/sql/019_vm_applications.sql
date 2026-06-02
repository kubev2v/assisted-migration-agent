CREATE TABLE IF NOT EXISTS vm_applications (
    app_name VARCHAR NOT NULL,
    app_desc VARCHAR NOT NULL,
    vm_id    VARCHAR NOT NULL,
    vm_name  VARCHAR NOT NULL,
    PRIMARY KEY (app_name, vm_id)
);
