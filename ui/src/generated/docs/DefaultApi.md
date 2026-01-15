# DefaultApi

All URIs are relative to */api/v1*

|Method | HTTP request | Description|
|------------- | ------------- | -------------|
|[**addVMsToInspection**](#addvmstoinspection) | **PATCH** /vms/inspector | Add more VMs to inspection queue|
|[**getAgentStatus**](#getagentstatus) | **GET** /agent | Get agent status|
|[**getCollectorStatus**](#getcollectorstatus) | **GET** /collector | Get collector status|
|[**getInspectorStatus**](#getinspectorstatus) | **GET** /vms/inspector | Get inspector status|
|[**getInventory**](#getinventory) | **GET** /inventory | Get collected inventory|
|[**getVM**](#getvm) | **GET** /vms/{id} | Get details about a vm|
|[**getVMInspectionStatus**](#getvminspectionstatus) | **GET** /vms/{id}/inspector | Get inspection status for a specific VM|
|[**getVMs**](#getvms) | **GET** /vms | Get list of VMs with filtering and pagination|
|[**removeVMsFromInspection**](#removevmsfrominspection) | **DELETE** /vms/inspector | Remove VMs from inspection queue or stop inspector entirely|
|[**setAgentMode**](#setagentmode) | **POST** /agent | Change agent mode|
|[**startCollector**](#startcollector) | **POST** /collector | Start inventory collection|
|[**startInspection**](#startinspection) | **POST** /vms/inspector | Start inspection for VMs|
|[**stopCollector**](#stopcollector) | **DELETE** /collector | Stop collection|

# **addVMsToInspection**
> InspectorStatus addVMsToInspection(requestBody)


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let requestBody: Array<number>; //

const { status, data } = await apiInstance.addVMsToInspection(
    requestBody
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **requestBody** | **Array<number>**|  | |


### Return type

**InspectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**202** | VMs added to inspection queue |  -  |
|**400** | Invalid request |  -  |
|**404** | Inspector not running |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

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

# **getInspectorStatus**
> InspectorStatus getInspectorStatus()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.getInspectorStatus();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**InspectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Inspector status |  -  |
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

# **getVM**
> VMDetails getVM()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; //VM id (default to undefined)

const { status, data } = await apiInstance.getVM(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] | VM id | defaults to undefined|


### Return type

**VMDetails**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | VM details |  -  |
|**404** | VM not found |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **getVMInspectionStatus**
> InspectionStatus getVMInspectionStatus()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: number; //VM ID (default to undefined)

const { status, data } = await apiInstance.getVMInspectionStatus(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**number**] | VM ID | defaults to undefined|


### Return type

**InspectionStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | VM inspection status |  -  |
|**404** | VM not found |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **getVMs**
> VMListResponse getVMs()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let minIssues: number; //Filter VMs with at least this many issues (optional) (default to undefined)
let clusters: Array<string>; //Filter by clusters (OR logic - matches VMs in any of the specified clusters) (optional) (default to undefined)
let diskSizeMin: number; //Minimum disk size in MB (optional) (default to undefined)
let diskSizeMax: number; //Maximum disk size in MB (optional) (default to undefined)
let memorySizeMin: number; //Minimum memory size in MB (optional) (default to undefined)
let memorySizeMax: number; //Maximum memory size in MB (optional) (default to undefined)
let status: Array<string>; //Filter by status (OR logic - matches VMs with any of the specified statuses) (optional) (default to undefined)
let sort: Array<string>; //Sort fields with direction (e.g., \"name:asc\" or \"cluster:desc,name:asc\"). Valid fields are name, vCenterState, cluster, diskSize, memory, issues. (optional) (default to undefined)
let page: number; //Page number for pagination (optional) (default to 1)
let pageSize: number; //Number of items per page (optional) (default to undefined)

const { status, data } = await apiInstance.getVMs(
    minIssues,
    clusters,
    diskSizeMin,
    diskSizeMax,
    memorySizeMin,
    memorySizeMax,
    status,
    sort,
    page,
    pageSize
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **minIssues** | [**number**] | Filter VMs with at least this many issues | (optional) defaults to undefined|
| **clusters** | **Array&lt;string&gt;** | Filter by clusters (OR logic - matches VMs in any of the specified clusters) | (optional) defaults to undefined|
| **diskSizeMin** | [**number**] | Minimum disk size in MB | (optional) defaults to undefined|
| **diskSizeMax** | [**number**] | Maximum disk size in MB | (optional) defaults to undefined|
| **memorySizeMin** | [**number**] | Minimum memory size in MB | (optional) defaults to undefined|
| **memorySizeMax** | [**number**] | Maximum memory size in MB | (optional) defaults to undefined|
| **status** | **Array&lt;string&gt;** | Filter by status (OR logic - matches VMs with any of the specified statuses) | (optional) defaults to undefined|
| **sort** | **Array&lt;string&gt;** | Sort fields with direction (e.g., \&quot;name:asc\&quot; or \&quot;cluster:desc,name:asc\&quot;). Valid fields are name, vCenterState, cluster, diskSize, memory, issues. | (optional) defaults to undefined|
| **page** | [**number**] | Page number for pagination | (optional) defaults to 1|
| **pageSize** | [**number**] | Number of items per page | (optional) defaults to undefined|


### Return type

**VMListResponse**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of VMs |  -  |
|**400** | Invalid request parameters |  -  |
|**500** | Internal server error |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **removeVMsFromInspection**
> InspectorStatus removeVMsFromInspection()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let requestBody: Array<number>; //Optional array of VM IDs to remove from queue. If not provided, stops the inspector entirely. (optional)

const { status, data } = await apiInstance.removeVMsFromInspection(
    requestBody
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **requestBody** | **Array<number>**| Optional array of VM IDs to remove from queue. If not provided, stops the inspector entirely. | |


### Return type

**InspectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | VMs removed from queue |  -  |
|**204** | Inspector stopped (when no request body provided) |  -  |
|**400** | Invalid request |  -  |
|**404** | Inspector not running |  -  |
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

# **startInspection**
> InspectorStatus startInspection(requestBody)


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from 'migration-agent-api-client';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let requestBody: Array<number>; //

const { status, data } = await apiInstance.startInspection(
    requestBody
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **requestBody** | **Array<number>**|  | |


### Return type

**InspectorStatus**

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**202** | Inspection started |  -  |
|**400** | Invalid request |  -  |
|**409** | Inspector already running |  -  |
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

