# VMNIC


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**mac** | **string** | MAC address of the virtual NIC | [optional] [default to undefined]
**network** | **string** | Reference to the network this NIC is connected to | [optional] [default to undefined]
**index** | **number** | Index of the NIC within the VM | [optional] [default to undefined]

## Example

```typescript
import { VMNIC } from 'migration-agent-api-client';

const instance: VMNIC = {
    mac,
    network,
    index,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
