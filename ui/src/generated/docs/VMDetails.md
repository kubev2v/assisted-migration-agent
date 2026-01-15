# VMDetails


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **string** | Unique identifier for the VM in vCenter | [default to undefined]
**name** | **string** | Display name of the VM | [default to undefined]
**uuid** | **string** | Universally unique identifier assigned by vCenter | [optional] [default to undefined]
**firmware** | **string** | Firmware type used by the VM (bios or efi) | [optional] [default to undefined]
**powerState** | **string** | Current power state of the VM (poweredOn, poweredOff, or suspended) | [default to undefined]
**connectionState** | **string** | State of the connection between vCenter and the VM\&#39;s host (connected, disconnected, orphaned, or inaccessible) | [default to undefined]
**host** | **string** | Reference to the ESXi host where the VM is running | [optional] [default to undefined]
**datacenter** | **string** | Name of the datacenter containing the VM | [optional] [default to undefined]
**cluster** | **string** | Name of the cluster containing the VM | [optional] [default to undefined]
**folder** | **string** | Reference to the inventory folder containing the VM | [optional] [default to undefined]
**cpuCount** | **number** | Total number of virtual CPUs allocated to the VM | [default to undefined]
**coresPerSocket** | **number** | Number of CPU cores per virtual socket | [default to undefined]
**cpuAffinity** | **Array&lt;number&gt;** | List of physical CPU IDs the VM is pinned to for scheduling | [optional] [default to undefined]
**memoryMB** | **number** | Amount of memory allocated to the VM in megabytes | [default to undefined]
**guestName** | **string** | Full name of the guest operating system as reported by VMware Tools | [optional] [default to undefined]
**guestId** | **string** | VMware identifier for the guest OS type (e.g., rhel8_64Guest) | [optional] [default to undefined]
**hostName** | **string** | Hostname of the guest OS as reported by VMware Tools | [optional] [default to undefined]
**ipAddress** | **string** | Primary IP address of the guest OS as reported by VMware Tools | [optional] [default to undefined]
**storageUsed** | **number** | Total storage space consumed by the VM in bytes | [optional] [default to undefined]
**isTemplate** | **boolean** | Whether the VM is a template rather than a regular VM | [optional] [default to undefined]
**faultToleranceEnabled** | **boolean** | Whether VMware Fault Tolerance is enabled, which maintains a live shadow VM for instant failover | [optional] [default to undefined]
**nestedHVEnabled** | **boolean** | Whether nested virtualization is enabled, allowing hypervisors to run inside the VM | [optional] [default to undefined]
**toolsStatus** | **string** | Installation status of VMware Tools (toolsNotInstalled, toolsNotRunning, toolsOld, toolsOk) | [optional] [default to undefined]
**toolsRunningStatus** | **string** | Whether VMware Tools is currently running in the guest OS | [optional] [default to undefined]
**disks** | [**Array&lt;VMDisk&gt;**](VMDisk.md) | List of virtual disks attached to the VM | [default to undefined]
**nics** | [**Array&lt;VMNIC&gt;**](VMNIC.md) | List of virtual network interface cards attached to the VM | [default to undefined]
**devices** | [**Array&lt;VMDevice&gt;**](VMDevice.md) | List of other virtual devices attached to the VM | [optional] [default to undefined]
**guestNetworks** | [**Array&lt;GuestNetwork&gt;**](GuestNetwork.md) | Network configuration inside the guest OS as reported by VMware Tools | [optional] [default to undefined]
**issues** | **Array&lt;string&gt;** | List of issue identifiers affecting this VM | [optional] [default to undefined]
**inspection** | [**InspectionStatus**](InspectionStatus.md) |  | [optional] [default to undefined]

## Example

```typescript
import { VMDetails } from 'migration-agent-api-client';

const instance: VMDetails = {
    id,
    name,
    uuid,
    firmware,
    powerState,
    connectionState,
    host,
    datacenter,
    cluster,
    folder,
    cpuCount,
    coresPerSocket,
    cpuAffinity,
    memoryMB,
    guestName,
    guestId,
    hostName,
    ipAddress,
    storageUsed,
    isTemplate,
    faultToleranceEnabled,
    nestedHVEnabled,
    toolsStatus,
    toolsRunningStatus,
    disks,
    nics,
    devices,
    guestNetworks,
    issues,
    inspection,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
