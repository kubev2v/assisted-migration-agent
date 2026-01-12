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
*DefaultApi* | [**getAgentStatus**](docs/DefaultApi.md#getagentstatus) | **GET** /agent | Get agent status
*DefaultApi* | [**getCollectorStatus**](docs/DefaultApi.md#getcollectorstatus) | **GET** /collector | Get collector status
*DefaultApi* | [**getInventory**](docs/DefaultApi.md#getinventory) | **GET** /inventory | Get collected inventory
*DefaultApi* | [**setAgentMode**](docs/DefaultApi.md#setagentmode) | **POST** /agent | Change agent mode
*DefaultApi* | [**startCollector**](docs/DefaultApi.md#startcollector) | **POST** /collector | Start inventory collection
*DefaultApi* | [**stopCollector**](docs/DefaultApi.md#stopcollector) | **DELETE** /collector | Stop collection


### Documentation For Models

 - [AgentModeRequest](docs/AgentModeRequest.md)
 - [AgentStatus](docs/AgentStatus.md)
 - [CollectorStartRequest](docs/CollectorStartRequest.md)
 - [CollectorStatus](docs/CollectorStatus.md)
 - [Datastore](docs/Datastore.md)
 - [DiskSizeTierSummary](docs/DiskSizeTierSummary.md)
 - [DiskTypeSummary](docs/DiskTypeSummary.md)
 - [Histogram](docs/Histogram.md)
 - [Host](docs/Host.md)
 - [Infra](docs/Infra.md)
 - [Inventory](docs/Inventory.md)
 - [InventoryData](docs/InventoryData.md)
 - [MigrationIssue](docs/MigrationIssue.md)
 - [Network](docs/Network.md)
 - [OsInfo](docs/OsInfo.md)
 - [VCenter](docs/VCenter.md)
 - [VMResourceBreakdown](docs/VMResourceBreakdown.md)
 - [VMs](docs/VMs.md)


<a id="documentation-for-authorization"></a>
## Documentation For Authorization

Endpoints do not require authorization.

