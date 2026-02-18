# API Reference

## VMs Endpoint

### GET /api/v1/vms

Returns a paginated list of VMs with filtering and sorting capabilities.

#### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `clusters` | array | Filter by cluster names (OR logic) |
| `status` | array | Filter by power state: `poweredOn`, `poweredOff`, `suspended` (OR logic) |
| `minIssues` | integer | Filter VMs with at least this many issues |
| `diskSizeMin` | integer | Minimum disk size in MB |
| `diskSizeMax` | integer | Maximum disk size in MB |
| `memorySizeMin` | integer | Minimum memory size in MB |
| `memorySizeMax` | integer | Maximum memory size in MB |
| `sort` | array | Sort fields with direction (e.g., `name:asc`, `cluster:desc`) |
| `page` | integer | Page number (default: 1) |
| `pageSize` | integer | Items per page |

**Valid sort fields:** `name`, `vCenterState`, `cluster`, `diskSize`, `memory`, `issues`

#### Examples

Get all VMs:

```bash
curl http://localhost:8000/api/v1/vms
```

Filter by cluster:

```bash
curl "http://localhost:8000/api/v1/vms?clusters=production&clusters=staging"
```

Filter by power state:

```bash
curl "http://localhost:8000/api/v1/vms?status=poweredOn"
```

Filter by disk size range (100GB to 500GB):

```bash
curl "http://localhost:8000/api/v1/vms?diskSizeMin=102400&diskSizeMax=512000"
```

Filter VMs with issues:

```bash
curl "http://localhost:8000/api/v1/vms?minIssues=1"
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
      "diskSize": 104857600,
      "memory": 4096,
      "issueCount": 0,
      "migratable": true,
      "template": false,
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
| `diskSize` | integer | Total disk size in bytes |
| `memory` | integer | Memory size in MB |
| `issueCount` | integer | Number of migration issues |
| `migratable` | boolean | `true` if VM has no critical issues |
| `template` | boolean | `true` if VM is a template |
| `inspection` | object | Inspection status |

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
