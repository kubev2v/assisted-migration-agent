-- Version 2 may encounter parser-owned objects. Remove only additions absent
-- from the baseline; never drop a pre-existing table or column and its data.
{{if not (index .Tables "vhostscsi")}}DROP TABLE IF EXISTS vhostscsi;{{end}}
{{if not (index .Tables "vhba")}}
DROP TABLE IF EXISTS vhba;
{{else}}
{{if not (index .Columns "vhba.Host ID")}}ALTER TABLE vhba DROP COLUMN IF EXISTS "Host ID";{{end}}
{{if not (index .Columns "vhba.Device")}}ALTER TABLE vhba DROP COLUMN IF EXISTS "Device";{{end}}
{{if not (index .Columns "vhba.Protocol")}}ALTER TABLE vhba DROP COLUMN IF EXISTS "Protocol";{{end}}
{{end}}
{{if not (index .Columns "vdisk.RDM LUN UUID")}}ALTER TABLE vdisk DROP COLUMN IF EXISTS "RDM LUN UUID";{{end}}
{{if not (index .Columns "vdatastore.Filesystem Type")}}ALTER TABLE vdatastore DROP COLUMN IF EXISTS "Filesystem Type";{{end}}
{{if not (index .Columns "vdatastore.Filesystem Major Version")}}ALTER TABLE vdatastore DROP COLUMN IF EXISTS "Filesystem Major Version";{{end}}
{{if not (index .Columns "vdatastore.Storage Protocol")}}ALTER TABLE vdatastore DROP COLUMN IF EXISTS "Storage Protocol";{{end}}
-- A baseline can already contain every optional object.
SELECT 1;
