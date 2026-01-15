# VM


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **string** | VM name | [default to undefined]
**id** | **string** | VM ID | [default to undefined]
**vCenterState** | **string** | vCenter state (e.g., poweredOn, poweredOff, suspended) | [default to undefined]
**cluster** | **string** | Cluster name | [default to undefined]
**diskSize** | **number** | Total disk size in MB | [default to undefined]
**memory** | **number** | Memory size in MB | [default to undefined]
**issueCount** | **number** | Number of issues found for this VM | [default to undefined]
**inspection** | [**InspectionStatus**](InspectionStatus.md) |  | [default to undefined]

## Example

```typescript
import { VM } from 'migration-agent-api-client';

const instance: VM = {
    name,
    id,
    vCenterState,
    cluster,
    diskSize,
    memory,
    issueCount,
    inspection,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
