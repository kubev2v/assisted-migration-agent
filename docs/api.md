# API Reference

## Version Endpoint

### GET /api/v1/version

Returns the agent version and git commit.

```bash
curl http://localhost:8000/api/v1/version
```

#### Response

```json
{
  "version": "v2.0.0",
  "gitCommit": "abc1234"
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Agent version (from `VERSION` file, e.g. `v2.0.0`) |
| `gitCommit` | string | Git commit SHA at build time |

## VMs Endpoint

### GET /api/v1/vms

Returns a paginated list of VMs with filtering and sorting capabilities.

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `byExpression` | string | Filter by expression (DSL). See [Filter by Expression](filter-by-expression.md) for grammar and all supported fields. |
| `sort` | array | Sort fields with direction (e.g., `name:asc`, `cluster:desc`) |
| `page` | integer | Page number (default: 1) |
| `pageSize` | integer | Items per page (default: 20, max: 100) |

**Valid sort fields:** `name`, `vCenterState`, `cluster`, `diskSize`, `memory`, `issues`

#### Examples

Get all VMs:

```bash
curl http://localhost:8000/api/v1/vms
```

Filter by expression:

```bash
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode "byExpression=cluster = 'production' and memory >= 8GB"
```

Sort by cluster ascending, then by name descending:

```bash
curl "http://localhost:8000/api/v1/vms?sort=cluster:asc&sort=name:desc"
```

Paginate results:

```bash
curl "http://localhost:8000/api/v1/vms?page=2&pageSize=10"
```

#### Response

```json
{
  "vms": [
    {
      "id": "vm-001",
      "name": "web-server-1",
      "vCenterState": "poweredOn",
      "cluster": "production",
      "datacenter": "DC1",
      "diskSize": 104857600,
      "memory": 4096,
      "issueCount": 0,
      "migratable": true,
      "template": false,
      "tags": ["production", "critical"],
      "inspection": {
        "state": "completed"
      }
    }
  ],
  "total": 42,
  "page": 1,
  "pageCount": 5
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | VM identifier |
| `name` | string | VM name |
| `vCenterState` | string | Power state (`poweredOn`, `poweredOff`, `suspended`) |
| `cluster` | string | Cluster name |
| `datacenter` | string | Datacenter name |
| `diskSize` | integer | Total disk size in bytes |
| `memory` | integer | Memory size in MB |
| `issueCount` | integer | Number of migration issues |
| `migratable` | boolean | `true` if VM has no critical issues |
| `template` | boolean | `true` if VM is a template |
| `tags` | array | Distinct tags from all groups whose filter matches this VM |
| `inspection` | object | Inspection status |

## Groups Endpoint

Groups are named filter expressions (with optional tags) that dynamically match VMs. When a group is created or updated, its filter is evaluated against the VM inventory and matching VM IDs are stored in a pre-computed `group_matches` table. This avoids re-evaluating filters on every read. Tags assigned to groups are surfaced on matching VMs in the `GET /vms` response.

### GET /api/v1/vms/groups

Returns a paginated list of groups with optional name filtering.

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `byName` | string | Filter groups by exact name match |
| `page` | integer | Page number (default: 1) |
| `pageSize` | integer | Items per page (default: 20, max: 100) |

#### Examples

List all groups:

```bash
curl http://localhost:8000/api/v1/vms/groups
```

Filter by name:

```bash
curl "http://localhost:8000/api/v1/vms/groups?byName=production"
```

Paginate:

```bash
curl "http://localhost:8000/api/v1/vms/groups?page=2&pageSize=10"
```

#### Response

```json
{
  "groups": [
    {
      "id": "1",
      "name": "Production VMs",
      "description": "All production workloads",
      "filter": "cluster = 'prod'",
      "tags": ["production"],
      "createdAt": "2025-01-01T00:00:00Z",
      "updatedAt": "2025-01-01T00:00:00Z"
    }
  ],
  "total": 5,
  "page": 1,
  "pageCount": 1
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `groups` | array | List of group objects |
| `total` | integer | Total number of groups matching the filter |
| `page` | integer | Current page number |
| `pageCount` | integer | Total number of pages |

#### Group Object

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Group identifier |
| `name` | string | Group name (unique, 1-100 characters) |
| `description` | string | Optional description (max 500 characters) |
| `filter` | string | Filter DSL expression evaluated against VMs |
| `tags` | array | Optional list of tags (each matching `[a-zA-Z0-9_.]+`) |
| `createdAt` | string | ISO 8601 creation timestamp |
| `updatedAt` | string | ISO 8601 last update timestamp |

### POST /api/v1/vms/groups

Creates a new group.

#### Request Body

```json
{
  "name": "Production VMs",
  "filter": "cluster = 'prod' and memory >= 8GB",
  "description": "All production workloads",
  "tags": ["production", "critical"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Group name, 1-100 characters (trimmed of whitespace) |
| `filter` | string | yes | Valid filter DSL expression (see [Filter by Expression](filter-by-expression.md)) |
| `description` | string | no | Optional description, max 500 characters |
| `tags` | array | no | List of tags, each matching `[a-zA-Z0-9_.]+` |

#### Examples

```bash
curl -X POST http://localhost:8000/api/v1/vms/groups \
  -H "Content-Type: application/json" \
  -d '{"name": "Large VMs", "filter": "memory >= 32GB and total_disk_capacity >= 500GB"}'
```

#### Response

**201 Created** — returns the created group object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Missing or empty name/filter, name > 100 chars, description > 500 chars, invalid filter expression, invalid tag format, duplicate name |

### GET /api/v1/vms/groups/{id}

Returns a group and its matching VMs with pagination and sorting. VMs are looked up from pre-computed matches (no filter re-evaluation at read time).

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `sort` | array | Sort fields with direction (e.g., `name:asc`, `memory:desc`) |
| `page` | integer | Page number (default: 1) |
| `pageSize` | integer | Items per page (default: 20, max: 100) |

**Valid sort fields:** `name`, `vCenterState`, `cluster`, `diskSize`, `memory`, `issues`

#### Examples

```bash
curl http://localhost:8000/api/v1/vms/groups/1
```

With sorting and pagination:

```bash
curl "http://localhost:8000/api/v1/vms/groups/1?sort=memory:desc&page=1&pageSize=10"
```

#### Response

```json
{
  "group": {
    "id": "1",
    "name": "Production VMs",
    "description": "All production workloads",
    "filter": "cluster = 'prod'",
    "tags": ["production"],
    "createdAt": "2025-01-01T00:00:00Z",
    "updatedAt": "2025-01-01T00:00:00Z"
  },
  "vms": [
    {
      "id": "vm-001",
      "name": "web-server-1",
      "vCenterState": "poweredOn",
      "cluster": "production",
      "datacenter": "DC1",
      "diskSize": 104857600,
      "memory": 4096,
      "issueCount": 0,
      "migratable": true,
      "template": false,
      "tags": ["production"]
    }
  ],
  "total": 42,
  "page": 1,
  "pageCount": 3
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `group` | object | The group object (see [Group Object](#group-object) above) |
| `vms` | array | VMs matching the group's filter (same schema as GET /vms) |
| `total` | integer | Total VMs matching the filter |
| `page` | integer | Current page number |
| `pageCount` | integer | Total number of pages |

#### Errors

| Status | Condition |
|--------|-----------|
| 404 | Group not found |

### PATCH /api/v1/vms/groups/{id}

Partially updates an existing group. At least one field must be provided.

#### Request Body

All fields are optional; only provided fields are updated.

```json
{
  "name": "Updated Name",
  "filter": "memory >= 16GB",
  "description": "updated description",
  "tags": ["staging"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | no | New name, 1-100 characters (trimmed of whitespace) |
| `filter` | string | no | New filter DSL expression |
| `description` | string | no | New description, max 500 characters |
| `tags` | array | no | New tags, each matching `[a-zA-Z0-9_.]+` |

#### Examples

Update only the filter:

```bash
curl -X PATCH http://localhost:8000/api/v1/vms/groups/1 \
  -H "Content-Type: application/json" \
  -d '{"filter": "memory >= 32GB"}'
```

#### Response

**200 OK** — returns the updated group object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | No fields provided, name > 100 chars, description > 500 chars, invalid filter expression, invalid tag format, duplicate name |
| 404 | Group not found |

### DELETE /api/v1/vms/groups/{id}

Deletes a group and its pre-computed matches. Idempotent — returns 204 even if the group does not exist.

#### Examples

```bash
curl -X DELETE http://localhost:8000/api/v1/vms/groups/1
```

#### Response

**204 No Content**

---

### GET /api/v1/vms/{id}

Returns detailed information about a specific VM including disks, NICs, and issues.

```bash
curl http://localhost:8000/api/v1/vms/vm-001
```

#### Response

```json
{
  "id": "vm-001",
  "name": "web-server-1",
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "firmware": "efi",
  "powerState": "poweredOn",
  "connectionState": "connected",
  "host": "esxi-01.local",
  "datacenter": "DC1",
  "cluster": "production",
  "folder": "/vms/web",
  "cpuCount": 4,
  "coresPerSocket": 2,
  "memoryMB": 8192,
  "guestName": "Red Hat Enterprise Linux 8",
  "guestId": "rhel8_64Guest",
  "hostName": "webserver1.local",
  "ipAddress": "192.168.1.100",
  "storageUsed": 107374182400,
  "template": false,
  "migratable": true,
  "faultToleranceEnabled": false,
  "nestedHVEnabled": false,
  "toolsStatus": "toolsOk",
  "toolsRunningStatus": "guestToolsRunning",
  "disks": [
    {
      "key": 2000,
      "file": "[datastore1] vm-001/disk1.vmdk",
      "capacity": 107374182400,
      "shared": false,
      "rdm": false,
      "bus": "scsi",
      "mode": "persistent"
    }
  ],
  "nics": [
    {
      "mac": "00:50:56:01:02:03",
      "network": "VM Network",
      "index": 0
    }
  ],
  "issues": ["ISSUE_001", "ISSUE_002"],
  "inspection": {
    "state": "completed"
  }
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the VM in vCenter |
| `name` | string | Display name of the VM |
| `uuid` | string | Universally unique identifier assigned by vCenter |
| `firmware` | string | Firmware type (`bios` or `efi`) |
| `powerState` | string | Current power state (`poweredOn`, `poweredOff`, `suspended`) |
| `connectionState` | string | Connection state (`connected`, `disconnected`, `orphaned`, `inaccessible`) |
| `host` | string | ESXi host where the VM is running |
| `datacenter` | string | Name of the datacenter containing the VM |
| `cluster` | string | Name of the cluster containing the VM |
| `folder` | string | Inventory folder path containing the VM |
| `cpuCount` | integer | Total number of virtual CPUs |
| `coresPerSocket` | integer | Number of CPU cores per virtual socket |
| `memoryMB` | integer | Memory allocated in megabytes |
| `guestName` | string | Guest OS name as reported by VMware Tools |
| `guestId` | string | VMware identifier for the guest OS type |
| `hostName` | string | Hostname of the guest OS |
| `ipAddress` | string | Primary IP address of the guest OS |
| `storageUsed` | integer | Total storage consumed in bytes |
| `template` | boolean | `true` if VM is a template |
| `migratable` | boolean | `true` if VM has no critical issues |
| `faultToleranceEnabled` | boolean | Whether VMware Fault Tolerance is enabled |
| `nestedHVEnabled` | boolean | Whether nested virtualization is enabled |
| `toolsStatus` | string | VMware Tools status (`toolsNotInstalled`, `toolsNotRunning`, `toolsOld`, `toolsOk`) |
| `toolsRunningStatus` | string | Whether VMware Tools is currently running |
| `disks` | array | List of virtual disks |
| `nics` | array | List of virtual NICs |
| `issues` | array | List of issue identifiers affecting this VM |
| `inspection` | object | Current inspection status |

#### Disk Object

| Field | Type | Description |
|-------|------|-------------|
| `key` | integer | Unique key identifying this disk within the VM |
| `file` | string | Path to the VMDK file in the datastore |
| `capacity` | integer | Disk capacity in bytes |
| `shared` | boolean | Whether disk is shared between multiple VMs |
| `rdm` | boolean | Whether this is a Raw Device Mapping |
| `bus` | string | Bus type (`scsi`, `ide`, `sata`, `nvme`) |
| `mode` | string | Disk mode (`persistent`, `independent_persistent`, `independent_nonpersistent`) |

#### NIC Object

| Field | Type | Description |
|-------|------|-------------|
| `mac` | string | MAC address of the virtual NIC |
| `network` | string | Network this NIC is connected to |
| `index` | integer | Index of the NIC within the VM |
