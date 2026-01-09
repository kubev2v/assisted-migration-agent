# DefaultApi

All URIs are relative to */api/v1*

|Method | HTTP request | Description|
|------------- | ------------- | -------------|
|[**getAgentStatus**](#getagentstatus) | **GET** /agent | Get agent status|
|[**getCollectorStatus**](#getcollectorstatus) | **GET** /collector | Get collector status|
|[**getInventory**](#getinventory) | **GET** /inventory | Get collected inventory|
|[**resetCollector**](#resetcollector) | **POST** /collector/reset | Reset collector state|
|[**setAgentMode**](#setagentmode) | **POST** /agent | Change agent mode|
|[**startCollector**](#startcollector) | **POST** /collector | Start inventory collection|
|[**stopCollector**](#stopcollector) | **DELETE** /collector | Stop collection|

# **getAgentStatus**
> AgentStatus getAgentStatus()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.getAgentStatus();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**AgentStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Agent status |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **getCollectorStatus**
> CollectorStatus getCollectorStatus()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.getCollectorStatus();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**CollectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Collector status |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **getInventory**
> Inventory getInventory()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.getInventory();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Inventory**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Collected inventory |  -  |
|**404** | Inventory not available |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **resetCollector**
> CollectorStatus resetCollector()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.resetCollector();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**CollectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Collection started |  -  |
|**400** | Invalid request |  -  |
|**409** | Collection already in progress |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **setAgentMode**
> AgentStatus setAgentMode(agentModeRequest)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    AgentModeRequest
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let agentModeRequest: AgentModeRequest; //

const { status, data } = await apiInstance.setAgentMode(
    agentModeRequest
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **agentModeRequest** | **AgentModeRequest**|  | |


### Return type

**AgentStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Mode changed |  -  |
|**400** | Invalid request |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **startCollector**
> CollectorStatus startCollector(collectorStartRequest)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    CollectorStartRequest
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let collectorStartRequest: CollectorStartRequest; //

const { status, data } = await apiInstance.startCollector(
    collectorStartRequest
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **collectorStartRequest** | **CollectorStartRequest**|  | |


### Return type

**CollectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**202** | Collection started |  -  |
|**400** | Invalid request |  -  |
|**409** | Collection already in progress |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **stopCollector**
> stopCollector()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.stopCollector();
```

### Parameters
This endpoint does not have any parameters.


### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**204** | Collection stopped |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

