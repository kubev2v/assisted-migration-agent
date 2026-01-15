# GuestNetwork


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**device** | **string** | Name of the network device inside the guest OS | [optional] [default to undefined]
**mac** | **string** | MAC address as seen by the guest OS | [optional] [default to undefined]
**ip** | **string** | IP address assigned to this interface | [optional] [default to undefined]
**prefixLength** | **number** | Network prefix length (subnet mask in CIDR notation) | [optional] [default to undefined]
**network** | **string** | Network name as reported by the guest OS | [optional] [default to undefined]

## Example

```typescript
import { GuestNetwork } from 'migration-agent-api-client';

const instance: GuestNetwork = {
    device,
    mac,
    ip,
    prefixLength,
    network,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
