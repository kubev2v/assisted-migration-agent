# API Reference

Base URL: `/api/v1`

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/agent` | [Get agent status](#get-apiv1agent) |
| POST | `/agent` | [Change agent mode](#post-apiv1agent) |
| GET | `/collector` | [Get collector status](#get-apiv1collector) |
| POST | `/collector` | [Start inventory collection](#post-apiv1collector) |
| DELETE | `/collector` | [Stop collection](#delete-apiv1collector) |
| GET | `/inventory` | [Get collected inventory](#get-apiv1inventory) |
| GET | `/version` | [Get agent version](#get-apiv1version) |
| GET | `/vms` | [List VMs (filtered, sorted, paginated)](#get-apiv1vms) |
| GET | `/vms/{id}` | [Get VM details](#get-apiv1vmsid) |
| POST | `/vms/{id}/inspection` | [Add VM to inspection queue](#post-apiv1vmsidinspection) |
| DELETE | `/vms/{id}/inspection` | [Remove VM from inspection queue](#delete-apiv1vmsidinspection) |
| GET | `/inspector` | [Get inspector status](#get-apiv1inspector) |
| POST | `/inspector` | [Start inspection](#post-apiv1inspector) |
| DELETE | `/inspector` | [Stop inspector](#delete-apiv1inspector) |
| PUT | `/inspector/credentials` | [Set vCenter credentials](#put-apiv1inspectorcredentials) |
| GET | `/inspector/vddk` | [Get VDDK status](#get-apiv1inspectorvddk) |
| PUT | `/inspector/vddk` | [Upload VDDK tarball](#put-apiv1inspectorvddk) |
| GET | `/groups` | [List groups](#get-apiv1groups) |
| POST | `/groups` | [Create group](#post-apiv1groups) |
| GET | `/groups/{id}` | [Get group with VMs](#get-apiv1groupsid) |
| PATCH | `/groups/{id}` | [Update group](#patch-apiv1groupsid) |
| DELETE | `/groups/{id}` | [Delete group](#delete-apiv1groupsid) |

---

## Agent

### GET /api/v1/agent

Returns the current agent status including console connection state.

```bash
curl http://localhost:8000/api/v1/agent
```

#### Response

```json
{
  "mode": "connected",
  "console_connection": "connected"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `mode` | string | Target mode: `connected` or `disconnected` |
| `console_connection` | string | Current console connection status: `connected` or `disconnected` |
| `error` | string | Connection error description (omitted when no error) |

### POST /api/v1/agent

Changes the agent mode.

```bash
curl -X POST http://localhost:8000/api/v1/agent \
  -H "Content-Type: application/json" \
  -d '{"mode": "connected"}'
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | yes | `connected` or `disconnected` |

#### Response

**200 OK** — returns the updated `AgentStatus` object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Invalid request |
| 409 | Mode conflict |

---

## Collector

### GET /api/v1/collector

Returns the collector status.

```bash
curl http://localhost:8000/api/v1/collector
```

#### Response

```json
{
  "status": "collected"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | `ready`, `connecting`, `collecting`, `parsing`, `collected`, or `error` |
| `error` | string | Error message (present only when status is `error`) |

### POST /api/v1/collector

Starts inventory collection with vCenter credentials.

```bash
curl -X POST http://localhost:8000/api/v1/collector \
  -H "Content-Type: application/json" \
  -d '{"url": "https://vcenter.local", "username": "admin", "password": "secret"}'
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | vCenter URL |
| `username` | string | yes | vCenter username |
| `password` | string | yes | vCenter password |

#### Response

**202 Accepted** — returns the `CollectorStatus` object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Invalid request |
| 409 | Collection already in progress |

### DELETE /api/v1/collector

Stops the current collection.

```bash
curl -X DELETE http://localhost:8000/api/v1/collector
```

#### Response

**200 OK** — returns the `CollectorStatus` object.

---

## Inventory

### GET /api/v1/inventory

Returns the collected inventory data.

```bash
curl http://localhost:8000/api/v1/inventory
```

#### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `withAgentId` | boolean | `false` | If `true`, wraps the inventory with the agent ID (compatible with manual inventory upload) |
| `group_id` | string | | Filter inventory to VMs matching this group's filter expression |

#### Errors

| Status | Condition |
|--------|-----------|
| 404 | Inventory not available (collection hasn't run yet) |

---

## Version

### GET /api/v1/version

Returns the agent version and git commit.

```bash
curl http://localhost:8000/api/v1/version
```

#### Response

```json
{
  "version": "v2.0.0",
  "gitCommit": "abc1234",
  "uiGitCommit": "def5678"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Agent version (e.g. `v2.0.0`) |
| `gitCommit` | string | Git commit SHA used to build the agent |
| `uiGitCommit` | string | Git commit SHA of the UI used to build the agent |

---

## VMs

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

Filter by substring match:

```bash
curl -G "http://localhost:8000/api/v1/vms" --data-urlencode "byExpression=name like 'prod'"
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
      "inspectionStatus": {
        "state": "completed"
      },
      "inspectionConcernCount": 2
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
| `diskSize` | integer | Total disk size in MB |
| `memory` | integer | Memory size in MB |
| `issueCount` | integer | Number of migration issues |
| `migratable` | boolean | `true` if VM has no critical issues |
| `template` | boolean | `true` if VM is a template |
| `tags` | array | Distinct tags from all groups whose filter matches this VM |
| `inspectionStatus` | object | Current inspection status (omitted if inspection was never started for this VM) |
| `inspectionConcernCount` | integer | Number of inspection concerns from the latest persisted result (omitted if zero) |

### GET /api/v1/vms/{id}

Returns detailed information about a specific VM including disks, NICs, devices, and issues.

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
  "devices": [
    { "kind": "cdrom" }
  ],
  "issues": [
    {
      "label": "SharedDisk",
      "category": "Critical",
      "description": "VM has shared disks which are not supported for migration"
    }
  ],
  "inspection": {
    "concerns": [
      {
        "label": "UnsupportedDriver",
        "category": "Warning",
        "message": "VM uses a driver that may not be available on the target platform"
      }
    ]
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
| `cpuAffinity` | array | List of physical CPU IDs the VM is pinned to (omitted if not set) |
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
| `disks` | array | List of virtual disks (see Disk Object) |
| `nics` | array | List of virtual NICs (see NIC Object) |
| `devices` | array | List of other virtual devices (see Device Object) |
| `guestNetworks` | array | Network configuration inside the guest OS (see Guest Network Object) |
| `issues` | array | List of issues affecting this VM (see Issue Object) |
| `inspection` | object | Inspection results with `concerns` array (omitted if no inspection results) |

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

#### Device Object

| Field | Type | Description |
|-------|------|-------------|
| `kind` | string | Type of virtual device (`cdrom`, `floppy`, `usb`, `serial`, `parallel`) |

#### Guest Network Object

| Field | Type | Description |
|-------|------|-------------|
| `device` | string | Name of the network device inside the guest OS |
| `mac` | string | MAC address as seen by the guest OS |
| `ip` | string | IP address assigned to this interface |
| `prefixLength` | integer | Network prefix length (CIDR notation) |
| `network` | string | Network name as reported by the guest OS |

#### Issue Object

| Field | Type | Description |
|-------|------|-------------|
| `label` | string | Short label describing the issue |
| `description` | string | Detailed description with context and recommendations |
| `category` | string | Severity: `Critical`, `Warning`, `Information`, `Advisory`, `Error`, or `Other` |

#### Inspection Results Object

| Field | Type | Description |
|-------|------|-------------|
| `concerns` | array | List of inspection concerns (see Inspection Concern) |

#### Inspection Concern

| Field | Type | Description |
|-------|------|-------------|
| `label` | string | Short label identifying the concern |
| `category` | string | Concern category |
| `message` | string | Detailed concern message |

### POST /api/v1/vms/{id}/inspection

Adds a VM to the inspection queue. The inspector must already be running.

```bash
curl -X POST http://localhost:8000/api/v1/vms/vm-001/inspection
```

#### Response

**202 Accepted** — returns the `InspectorStatus` object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Inspector not running, or inspection limit reached |

### DELETE /api/v1/vms/{id}/inspection

Removes a VM from the inspection queue (cancels its inspection).

```bash
curl -X DELETE http://localhost:8000/api/v1/vms/vm-001/inspection
```

#### Response

**200 OK** — returns the `VmInspectionStatus` for the canceled VM.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Inspector not running or VM cannot be canceled |

---

## Inspector

The inspector performs deep inspection of VMs via VDDK. Before starting an inspection you must:
1. Upload a VDDK tarball (`PUT /inspector/vddk`)
2. Set vCenter credentials (`PUT /inspector/credentials`)

### GET /api/v1/inspector

Returns the current inspector status. Optionally includes VDDK metadata and/or configured credentials.

#### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `includeVddk` | boolean | `false` | Include uploaded VDDK metadata (`version`, `md5`). Omitted from response if VDDK was never uploaded. |
| `includeCredentials` | boolean | `false` | Include configured vCenter URL and username (password is never returned). Omitted if credentials were never set. |

#### Examples

Basic status:

```bash
curl http://localhost:8000/api/v1/inspector
```

With VDDK and credentials:

```bash
curl "http://localhost:8000/api/v1/inspector?includeVddk=true&includeCredentials=true"
```

#### Response

```json
{
  "state": "running",
  "credentials": {
    "url": "https://vcenter.local",
    "username": "admin"
  },
  "vddk": {
    "version": "8.0.2",
    "md5": "d41d8cd98f00b204e9800998ecf8427e"
  }
}
```

#### InspectorStatus Object

| Field | Type | Description |
|-------|------|-------------|
| `state` | string | `ready`, `Initiating`, `running`, `canceled`, `completed`, or `error` |
| `error` | string | Error message (present only when state is `error`) |
| `credentials` | object | vCenter URL and username (only when `includeCredentials=true` and credentials are set; password is never returned) |
| `vddk` | object | VDDK properties (only when `includeVddk=true` and VDDK was uploaded) |

#### Inspector States

| State | Description |
|-------|-------------|
| `ready` | Inspector is idle, ready to start |
| `Initiating` | Inspector is initializing (connecting to vCenter, preparing pipelines) |
| `running` | Inspection is in progress |
| `canceled` | Inspection was stopped by the user |
| `completed` | All queued VMs have been inspected |
| `error` | Inspector encountered an error |

### POST /api/v1/inspector

Starts inspection for a list of VMs. Requires VDDK to be uploaded and credentials to be set beforehand.

```bash
curl -X POST http://localhost:8000/api/v1/inspector \
  -H "Content-Type: application/json" \
  -d '{"vmIds": ["vm-001", "vm-002", "vm-003"]}'
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vmIds` | array | yes | List of VM identifiers to inspect |

#### Response

**202 Accepted** — returns `InspectorStatus` with state `Initiating`.

```json
{
  "state": "Initiating"
}
```

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Empty `vmIds`, VDDK not uploaded, credentials not set, or inspection limit reached |
| 409 | Inspector already running |

### DELETE /api/v1/inspector

Stops the inspector entirely, canceling all queued and in-progress inspections.

```bash
curl -X DELETE http://localhost:8000/api/v1/inspector
```

#### Response

**202 Accepted** — returns the current `InspectorStatus`.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Already in canceling state |
| 404 | Inspector not running |

---

## Inspector Credentials

### PUT /api/v1/inspector/credentials

Sets or replaces the vCenter credentials used by the inspector. The agent validates the credentials by attempting a connection to vCenter.

```bash
curl -X PUT http://localhost:8000/api/v1/inspector/credentials \
  -H "Content-Type: application/json" \
  -d '{"url": "https://vcenter.local", "username": "admin", "password": "secret"}'
```

#### Request Body

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | yes | vCenter URL (must be a valid URL) |
| `username` | string | yes | vCenter username |
| `password` | string | yes | vCenter password |

#### Response

**200 OK** — empty body on success.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Invalid credentials (validation failed) or cannot connect to vCenter |

---

## Inspector VDDK

### GET /api/v1/inspector/vddk

Returns the properties of the uploaded VDDK tarball.

```bash
curl http://localhost:8000/api/v1/inspector/vddk
```

#### Response

```json
{
  "version": "8.0.2",
  "md5": "d41d8cd98f00b204e9800998ecf8427e"
}
```

#### VddkProperties Object

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | The matching vSphere Client version |
| `md5` | string | MD5 checksum of the uploaded tarball |
| `bytes` | integer | Size of the uploaded tarball in bytes (included on upload response) |

#### Errors

| Status | Condition |
|--------|-----------|
| 404 | VDDK has not been uploaded |

### PUT /api/v1/inspector/vddk

Uploads a VDDK tarball. Cannot be called while the inspector is running. Maximum file size is 64 MB.

```bash
curl -X PUT http://localhost:8000/api/v1/inspector/vddk \
  -F "file=@VMware-vix-disklib-8.0.2.tar.gz"
```

#### Request Body

`multipart/form-data` with a single `file` field containing the VDDK tarball.

#### Response

**200 OK** — returns `VddkProperties` including the `bytes` field.

```json
{
  "version": "8.0.2",
  "md5": "d41d8cd98f00b204e9800998ecf8427e",
  "bytes": 52428800
}
```

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Bad request or inspector is currently running |
| 409 | Upload already in progress |
| 413 | File exceeds 64 MB limit |

---

## VmInspectionStatus Object

Returned on per-VM inspection endpoints and embedded in VM list responses.

| Field | Type | Description |
|-------|------|-------------|
| `state` | string | `pending`, `running`, `completed`, `canceled`, or `error` |
| `error` | string | Error message (present only when state is `error`) |
| `results` | object | Inspection results (present only when state is `completed`) |

---

## Groups

Groups are named filter expressions (with optional tags) that dynamically match VMs. When a group is created or updated, its filter is evaluated against the VM inventory and matching VM IDs are stored in a pre-computed `group_matches` table. This avoids re-evaluating filters on every read. Tags assigned to groups are surfaced on matching VMs in the `GET /vms` response.

### GET /api/v1/groups

Returns a paginated list of groups with optional name filtering.

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `byName` | string | Filter groups by name (case-insensitive substring match) |
| `page` | integer | Page number (default: 1) |
| `pageSize` | integer | Items per page (default: 20, max: 100) |

#### Examples

List all groups:

```bash
curl http://localhost:8000/api/v1/groups
```

Filter by name:

```bash
curl "http://localhost:8000/api/v1/groups?byName=production"
```

Paginate:

```bash
curl "http://localhost:8000/api/v1/groups?page=2&pageSize=10"
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

### POST /api/v1/groups

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
curl -X POST http://localhost:8000/api/v1/groups \
  -H "Content-Type: application/json" \
  -d '{"name": "Large VMs", "filter": "memory >= 32GB and total_disk_capacity >= 500GB"}'
```

#### Response

**201 Created** — returns the created group object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Missing or empty name/filter, name > 100 chars, description > 500 chars, invalid filter expression, invalid tag format, duplicate name |

### GET /api/v1/groups/{id}

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
curl http://localhost:8000/api/v1/groups/1
```

With sorting and pagination:

```bash
curl "http://localhost:8000/api/v1/groups/1?sort=memory:desc&page=1&pageSize=10"
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

### PATCH /api/v1/groups/{id}

Partially updates an existing group. Only provided fields are updated.

#### Request Body

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
curl -X PATCH http://localhost:8000/api/v1/groups/1 \
  -H "Content-Type: application/json" \
  -d '{"filter": "memory >= 32GB"}'
```

#### Response

**200 OK** — returns the updated group object.

#### Errors

| Status | Condition |
|--------|-----------|
| 400 | Name > 100 chars, description > 500 chars, invalid filter expression, invalid tag format, duplicate name |
| 404 | Group not found |

### DELETE /api/v1/groups/{id}

Deletes a group and its pre-computed matches. Idempotent — returns 204 even if the group does not exist.

#### Examples

```bash
curl -X DELETE http://localhost:8000/api/v1/groups/1
```

#### Response

**204 No Content**
