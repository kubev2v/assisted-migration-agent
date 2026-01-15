# VMDisk


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**key** | **number** | Unique key identifying this disk within the VM | [optional] [default to undefined]
**file** | **string** | Path to the VMDK file in the datastore | [optional] [default to undefined]
**capacity** | **number** | Disk capacity in bytes | [optional] [default to undefined]
**shared** | **boolean** | Whether this disk is shared between multiple VMs | [optional] [default to undefined]
**rdm** | **boolean** | Whether this is a Raw Device Mapping (direct LUN access) | [optional] [default to undefined]
**bus** | **string** | Bus type (e.g., scsi, ide, sata, nvme) | [optional] [default to undefined]
**mode** | **string** | Disk mode (e.g., persistent, independent_persistent, independent_nonpersistent) | [optional] [default to undefined]

## Example

```typescript
import { VMDisk } from 'migration-agent-api-client';

const instance: VMDisk = {
    key,
    file,
    capacity,
    shared,
    rdm,
    bus,
    mode,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
