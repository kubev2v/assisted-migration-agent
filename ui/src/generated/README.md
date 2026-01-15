## migration-agent-api-client@1.0.0

This generator creates TypeScript/JavaScript client that utilizes [axios](https://github.com/axios/axios). The generated Node module can be used in the following environments:

Environment
* Node.js
* Webpack
* Browserify

Language level
* ES5 - you must have a Promises/A+ library installed
* ES6

Module system
* CommonJS
* ES6 module system

It can be used in both TypeScript and JavaScript. In TypeScript, the definition will be automatically resolved via `package.json`. ([Reference](https://www.typescriptlang.org/docs/handbook/declaration-files/consumption.html))

### Building

To build and compile the typescript sources to javascript use:
```
npm install
npm run build
```

### Publishing

First build the package then run `npm publish`

### Consuming

navigate to the folder of your consuming project and run one of the following commands.

_published:_

```
npm install migration-agent-api-client@1.0.0 --save
```

_unPublished (not recommended):_

```
npm install PATH_TO_GENERATED_PACKAGE --save
```

### Documentation for API Endpoints

All URIs are relative to */api/v1*

Class | Method | HTTP request | Description
------------ | ------------- | ------------- | -------------
*DefaultApi* | [**addVMsToInspection**](docs/DefaultApi.md#addvmstoinspection) | **PATCH** /vms/inspector | Add more VMs to inspection queue
*DefaultApi* | [**getAgentStatus**](docs/DefaultApi.md#getagentstatus) | **GET** /agent | Get agent status
*DefaultApi* | [**getCollectorStatus**](docs/DefaultApi.md#getcollectorstatus) | **GET** /collector | Get collector status
*DefaultApi* | [**getInspectorStatus**](docs/DefaultApi.md#getinspectorstatus) | **GET** /vms/inspector | Get inspector status
*DefaultApi* | [**getInventory**](docs/DefaultApi.md#getinventory) | **GET** /inventory | Get collected inventory
*DefaultApi* | [**getVM**](docs/DefaultApi.md#getvm) | **GET** /vms/{id} | Get details about a vm
*DefaultApi* | [**getVMInspectionStatus**](docs/DefaultApi.md#getvminspectionstatus) | **GET** /vms/{id}/inspector | Get inspection status for a specific VM
*DefaultApi* | [**getVMs**](docs/DefaultApi.md#getvms) | **GET** /vms | Get list of VMs with filtering and pagination
*DefaultApi* | [**removeVMsFromInspection**](docs/DefaultApi.md#removevmsfrominspection) | **DELETE** /vms/inspector | Remove VMs from inspection queue or stop inspector entirely
*DefaultApi* | [**setAgentMode**](docs/DefaultApi.md#setagentmode) | **POST** /agent | Change agent mode
*DefaultApi* | [**startCollector**](docs/DefaultApi.md#startcollector) | **POST** /collector | Start inventory collection
*DefaultApi* | [**startInspection**](docs/DefaultApi.md#startinspection) | **POST** /vms/inspector | Start inspection for VMs
*DefaultApi* | [**stopCollector**](docs/DefaultApi.md#stopcollector) | **DELETE** /collector | Stop collection


### Documentation For Models

 - [AgentModeRequest](docs/AgentModeRequest.md)
 - [AgentStatus](docs/AgentStatus.md)
 - [CollectorStartRequest](docs/CollectorStartRequest.md)
 - [CollectorStatus](docs/CollectorStatus.md)
 - [Datastore](docs/Datastore.md)
 - [DiskSizeTierSummary](docs/DiskSizeTierSummary.md)
 - [DiskTypeSummary](docs/DiskTypeSummary.md)
 - [GuestNetwork](docs/GuestNetwork.md)
 - [Histogram](docs/Histogram.md)
 - [Host](docs/Host.md)
 - [Infra](docs/Infra.md)
 - [InspectionStatus](docs/InspectionStatus.md)
 - [InspectorStatus](docs/InspectorStatus.md)
 - [Inventory](docs/Inventory.md)
 - [InventoryData](docs/InventoryData.md)
 - [MigrationIssue](docs/MigrationIssue.md)
 - [Network](docs/Network.md)
 - [OsInfo](docs/OsInfo.md)
 - [VCenter](docs/VCenter.md)
 - [VM](docs/VM.md)
 - [VMDetails](docs/VMDetails.md)
 - [VMDevice](docs/VMDevice.md)
 - [VMDisk](docs/VMDisk.md)
 - [VMListResponse](docs/VMListResponse.md)
 - [VMNIC](docs/VMNIC.md)
 - [VMResourceBreakdown](docs/VMResourceBreakdown.md)
 - [VMs](docs/VMs.md)


<a id="documentation-for-authorization"></a>
## Documentation For Authorization

Endpoints do not require authorization.

