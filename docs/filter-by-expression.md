# Filter by Expression (GET /vms)

The **GET /api/v1/vms** endpoint supports a `byExpression` query parameter that filters VMs using a small DSL. The expression is parsed and translated into SQL conditions against the VM store (backed by the default field mapping in `pkg/filter`).

## How to use

Pass the expression as the **byExpression** query parameter. The value must be URL-encoded when it contains spaces, quotes, or special characters.

**Example requests:**

```bash
# VMs with memory > 8GB
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode 'byExpression=memory > 8GB'

# VMs in specific clusters (expression)
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode "byExpression=cluster in ['prod', 'staging']"

# Combined: poweredOn and memory >= 16GB
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode "byExpression=status = 'poweredOn' and memory >= 16GB"

# Regex: names starting with "web-"
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode "byExpression=name ~ /^web-/"

# With pagination
curl -G "http://localhost:8000/api/v1/vms" \
  --data-urlencode "byExpression=cluster = 'DC1' and template = false" \
  --data-urlencode "page=1" \
  --data-urlencode "pageSize=20"
```

- **Parameter name:** `byExpression`
- **Type:** string (single expression)
- **Encoding:** Use `--data-urlencode` (or percent-encode) so spaces and quotes are safe.

`byExpression` can be combined with other query parameters (`sort`, `page`, `pageSize`). The expression filter is the only way to filter VMs.

---

## Expression grammar (summary)

- **Comparisons:** `field = value`, `!=`, `<`, `<=`, `>`, `>=`
- **Regex:** `field ~ /pattern/`, `field !~ /pattern/`
- **Lists:** `field in ['a','b']`, `field not in ['a','b']`
- **Logic:** `and`, `or`; use `( ... )` to group. AND binds tighter than OR.

**Value types:**

- **Strings:** `'...'` or `"..."` (empty allowed)
- **Booleans:** `true`, `false` (case-insensitive)
- **Quantities:** `123`, `8GB`, `512MB`, `1TB` (normalized to MB for comparison)
- **Regex:** `/pattern/` (escape `/` as `\/`)

**Examples:**

```text
name = 'web-server-01'
status != 'poweredOff'
memory >= 16GB
template = false
name ~ /^prod-/
cluster in ['prod', 'staging']
(cluster = 'prod' or cluster = 'staging') and concern.category != 'Critical'
```

---

## Filter fields (default mapping)

The following table lists every identifier supported by the default map function used for `byExpression`. Unknown identifiers cause a parse/validation error.

Identifiers are **case-insensitive**. Dotted names refer to joined tables (e.g. `disk.*`, `concern.*`).

### vinfo (VM core) — flat identifiers

| Identifier       | Type    | Description (backing column)        |
|------------------|---------|-------------------------------------|
| `id`             | string  | VM ID                               |
| `name`           | string  | VM name                             |
| `folder_id`      | string  | Folder ID                           |
| `folder`         | string  | Folder path                         |
| `host`           | string  | Host                                |
| `smbios_uuid`    | string  | SMBIOS UUID                         |
| `vm_uuid`        | string  | VM UUID                             |
| `firmware`       | string  | Firmware (e.g. bios, efi)           |
| `powerstate`     | string  | Power state                         |
| `status`         | string  | Alias for `powerstate`              |
| `connection_state` | string | Connection state                 |
| `ft_state`       | string  | FT state                            |
| `cpus`           | integer | CPU count                           |
| `memory`         | integer | Memory (MB; use quantity in expression e.g. 8GB) |
| `os_config`      | string  | OS according to configuration file |
| `os_tools`       | string  | OS according to VMware Tools        |
| `dns_name`       | string  | DNS name                            |
| `ip_address`     | string  | Primary IP address                  |
| `storage_used`   | integer | Storage in use (MiB)                |
| `template`       | boolean | Template flag                      |
| `cbt`            | boolean | CBT                                 |
| `enable_uuid`    | boolean | Enable UUID                         |
| `datacenter`     | string  | Datacenter                          |
| `cluster`        | string  | Cluster                             |
| `hw_version`     | string  | Hardware version                    |
| `total_disk_capacity` | integer | Total disk capacity (MiB)     |
| `provisioned`    | integer | Provisioned (MiB)                   |
| `resource_pool`  | string  | Resource pool                       |
| `issues_count`   | integer | Number of concerns/issues for the VM |

### vdisk (disk.*) — disk attributes

| Identifier        | Type    | Description (backing column) |
|-------------------|---------|-----------------------------|
| `disk.key`        | integer | Disk key                    |
| `disk.path`       | string  | Disk path                   |
| `disk.capacity`   | integer | Total disk capacity (MiB, aggregated sum of all disks) |
| `disk.sharing`    | string  | Sharing mode                |
| `disk.raw`        | boolean | Raw                         |
| `disk.shared_bus` | string  | Shared bus                  |
| `disk.mode`       | string  | Disk mode                   |
| `disk.thin`       | boolean | Thin                        |
| `disk.controller` | string  | Controller                  |
| `disk.label`      | string  | Label                       |

### concerns (concern.*) — concern/issue attributes

| Identifier         | Type   | Description (backing column) |
|--------------------|--------|-----------------------------|
| `concern.label`    | string | Label                       |
| `concern.category` | string | Category                    |
| `concern.assessment` | string | Assessment               |

### vm_inspection_status (inspection.*)

| Identifier           | Type   | Description (backing column) |
|----------------------|--------|-----------------------------|
| `inspection.status`  | string | Inspection status           |
| `inspection.error`   | string | Inspection error            |

### vcpu (cpu.*) — CPU attributes

| Identifier            | Type    | Description (backing column) |
|-----------------------|---------|-----------------------------|
| `cpu.hot_add`         | boolean | Hot add                     |
| `cpu.hot_remove`      | boolean | Hot remove                  |
| `cpu.sockets`         | integer | Sockets                     |
| `cpu.cores_per_socket`| integer | Cores per socket            |

### vmemory (mem.*) — memory attributes

| Identifier     | Type    | Description (backing column) |
|----------------|---------|-----------------------------|
| `mem.hot_add`  | boolean | Hot add                     |
| `mem.ballooned`| integer | Ballooned (MiB)             |

### vnetwork (net.*) — network attributes

| Identifier          | Type    | Description (backing column) |
|---------------------|---------|-----------------------------|
| `net.network`       | string  | Network                     |
| `net.mac`           | string  | MAC address                 |
| `net.nic_label`     | string  | NIC label                   |
| `net.adapter`       | string  | Adapter                     |
| `net.switch`        | string  | Switch                      |
| `net.connected`     | boolean | Connected                   |
| `net.starts_connected` | boolean | Starts connected         |
| `net.type`          | string  | Type                        |
| `net.ipv4`          | string  | IPv4 address                |
| `net.ipv6`          | string  | IPv6 address                |
| `net.cluster`       | string  | Cluster                     |

### vdatastore (datastore.*) — datastore attributes

| Identifier           | Type    | Description (backing column) |
|----------------------|---------|-----------------------------|
| `datastore.name`     | string  | Name                        |
| `datastore.hosts`    | string  | Hosts                       |
| `datastore.address`  | string  | Address                     |
| `datastore.object_id`| string  | Object ID                   |
| `datastore.free`     | integer | Free (MiB)                  |
| `datastore.mha`      | string  | MHA                         |
| `datastore.capacity` | integer | Capacity (MiB)              |
| `datastore.type`     | string  | Type                        |

---

## Operators

| Operator | Meaning                          | Example                    |
|----------|----------------------------------|----------------------------|
| `=`      | Equal                            | `status = 'poweredOn'`     |
| `!=`     | Not equal                        | `template != true`         |
| `>`      | Greater than                     | `memory > 8GB`             |
| `>=`     | Greater than or equal            | `cpus >= 4`                |
| `<`      | Less than                        | `storage_used < 1000`      |
| `<=`     | Less than or equal               | `memory <= 16GB`           |
| `~`      | Regex match                      | `name ~ /^prod-/`          |
| `!~`     | Regex not match                  | `name !~ /test/`           |
| `in`     | Value in list                    | `cluster in ['a','b']`     |
| `not in` | Value not in list                | `status not in ['suspended']` |
| `and`    | Logical AND                      | `a = '1' and b = '2'`      |
| `or`     | Logical OR                       | `cluster = 'prod' or cluster = 'staging'` |

---

## Errors

- **Invalid expression syntax:** API returns 400 with an error message; the filter package may return a parse error with position.
- **Unknown field in expression:** e.g. `unknown_field = 'x'` → error like `unknown filter field: unknown_field`.

For more detail on the grammar and the default mapping, see package `pkg/filter` (e.g. `doc.go` and `sql.go`).
