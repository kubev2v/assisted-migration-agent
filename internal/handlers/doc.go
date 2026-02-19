// Package handlers implements the HTTP API layer for the assisted-migration-agent.
//
// This package contains HTTP handlers that expose the agent's functionality via
// a RESTful API. Handlers delegate business logic to the services layer and focus
// on request validation, response formatting, and HTTP semantics.
//
// # Architecture Overview
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                     HTTP Request (Gin)                          │
//	└─────────────────────────────────────────────────────────────────┘
//	                              │
//	                              ▼
//	┌─────────────────────────────────────────────────────────────────┐
//	│                      Handler (this package)                     │
//	│  - Request validation                                           │
//	│  - Parameter parsing                                            │
//	│  - Error mapping to HTTP status codes                           │
//	│  - Model-to-API conversion                                      │
//	└─────────────────────────────────────────────────────────────────┘
//	                              │
//	                              ▼
//	┌─────────────────────────────────────────────────────────────────┐
//	│                      Services Layer                             │
//	│  Console │ Collector │ Inventory │ VM                           │
//	└─────────────────────────────────────────────────────────────────┘
//
// # Handler Structure
//
// All handlers are methods on a single Handler struct that holds service dependencies:
//
//	type Handler struct {
//	    consoleSrv   *services.Console
//	    collectorSrv *services.CollectorService
//	    inventorySrv *services.InventoryService
//	    vmSrv        *services.VMService
//	}
//
// The Handler implements the ServerInterface generated from the OpenAPI spec,
// enabling automatic route registration via:
//
//	v1.RegisterHandlers(router, handler)
//
// # API Endpoints
//
// Agent Endpoints (console.go):
//
//	┌────────┬──────────┬─────────────────────────────────────────────┐
//	│ Method │ Endpoint │ Description                                 │
//	├────────┼──────────┼─────────────────────────────────────────────┤
//	│ GET    │ /agent   │ Get agent status (connection state, mode)   │
//	│ POST   │ /agent   │ Set agent mode (connected/disconnected)     │
//	└────────┴──────────┴─────────────────────────────────────────────┘
//
// Collector Endpoints (collector.go):
//
//	┌────────┬─────────────┬──────────────────────────────────────────┐
//	│ Method │ Endpoint    │ Description                              │
//	├────────┼─────────────┼──────────────────────────────────────────┤
//	│ GET    │ /collector  │ Get collector status                     │
//	│ POST   │ /collector  │ Start inventory collection               │
//	│ DELETE │ /collector  │ Stop ongoing collection                  │
//	└────────┴─────────────┴──────────────────────────────────────────┘
//
// Inventory Endpoints (inventory.go):
//
//	┌────────┬─────────────┬──────────────────────────────────────────┐
//	│ Method │ Endpoint    │ Description                              │
//	├────────┼─────────────┼──────────────────────────────────────────┤
//	│ GET    │ /inventory  │ Get collected inventory as JSON          │
//	└────────┴─────────────┴──────────────────────────────────────────┘
//
// VM Endpoints (vms.go):
//
//	┌────────┬──────────────────┬───────────────────────────────────────┐
//	│ Method │ Endpoint         │ Description                           │
//	├────────┼──────────────────┼───────────────────────────────────────┤
//	│ GET    │ /vms             │ List VMs with filtering/pagination    │
//	│ GET    │ /vms/{id}        │ Get VM details                        │
//	│ GET    │ /vms/inspector   │ Get inspector status (not implemented)│
//	│ POST   │ /vms/inspector   │ Start inspection (not implemented)    │
//	│ PATCH  │ /vms/inspector   │ Add VMs to inspection (not impl.)     │
//	│ DELETE │ /vms/inspector   │ Remove VMs from inspection (not impl.)│
//	└────────┴──────────────────┴───────────────────────────────────────┘
//
// VDDK Endpoints (vddk.go):
//
//	┌────────┬──────────────────┬───────────────────────────────────────┐
//	│ Method │ Endpoint         │ Description                           │
//	├────────┼──────────────────┼───────────────────────────────────────┤
//	│ POST   │ /vddk            │ Upload VDDK tarball (max 64MB)        │
//	└────────┴──────────────────┴───────────────────────────────────────┘
//
// # Agent Handler
//
// GET /agent - Returns current agent status:
//
//	{
//	    "consoleConnection": "connected",  // current connection state
//	    "mode": "connected",               // target mode
//	    "error": null                      // optional error message
//	}
//
// POST /agent - Changes agent mode:
//
// Request:
//
//	{ "mode": "connected" }  // or "disconnected"
//
// Response: Same as GET /agent
//
// Errors:
//   - 400 Bad Request: Invalid mode value
//   - 409 Conflict: Mode change blocked after fatal console error
//
// # Collector Handler
//
// GET /collector - Returns collector status:
//
//	{
//	    "status": "collected",  // ready|connecting|collecting|collected|error
//	    "error": null           // optional error message
//	}
//
// POST /collector - Starts inventory collection:
//
// Request:
//
//	{
//	    "url": "https://vcenter.example.com",
//	    "username": "admin@vsphere.local",
//	    "password": "secret"
//	}
//
// Validation:
//   - All fields required
//   - URL must have valid scheme and host
//
// Response: 202 Accepted with collector status
//
// Errors:
//   - 400 Bad Request: Missing fields or invalid URL format
//   - 409 Conflict: Collection already in progress
//
// DELETE /collector - Stops ongoing collection, returns to ready state.
//
// # Inventory Handler
//
// GET /inventory - Returns raw inventory JSON.
//
// Errors:
//   - 404 Not Found: Inventory not yet collected
//
// # VM Handler
//
// GET /vms - Lists VMs with filtering, sorting, and pagination.
//
// Query Parameters:
//
//	┌────────────────┬──────────┬─────────────────────────────────────────┐
//	│ Parameter      │ Type     │ Description                             │
//	├────────────────┼──────────┼─────────────────────────────────────────┤
//	│ clusters       │ []string │ Filter by cluster names (OR logic)      │
//	│ status         │ []string │ Filter by power state (OR logic)        │
//	│ minIssues      │ int      │ Filter by minimum issue count           │
//	│ diskSizeMin    │ int64    │ Minimum disk size in MB                 │
//	│ diskSizeMax    │ int64    │ Maximum disk size in MB                 │
//	│ memorySizeMin  │ int64    │ Minimum memory in MB                    │
//	│ memorySizeMax  │ int64    │ Maximum memory in MB                    │
//	│ sort           │ []string │ Sort fields (format: "field:direction") │
//	│ page           │ int      │ Page number (default: 1)                │
//	│ pageSize       │ int      │ Items per page (default: 20, max: 100)  │
//	└────────────────┴──────────┴─────────────────────────────────────────┘
//
// Valid Sort Fields:
//   - name, vCenterState, cluster, diskSize, memory, issues
//
// Sort Direction:
//   - asc (ascending) or desc (descending)
//
// Example: /vms?clusters=prod&status=poweredOn&sort=name:asc&page=1&pageSize=50
//
// Response:
//
//	{
//	    "page": 1,
//	    "pageCount": 5,
//	    "total": 100,
//	    "vms": [
//	        {
//	            "id": "vm-123",
//	            "name": "web-server-01",
//	            "cluster": "prod-cluster",
//	            "vCenterState": "poweredOn",
//	            "diskSize": 102400,
//	            "memory": 8192,
//	            "issueCount": 0
//	        }
//	    ]
//	}
//
// Validation Errors (400 Bad Request):
//   - diskSizeMin > diskSizeMax
//   - memorySizeMin > memorySizeMax
//   - Invalid sort format (must be "field:direction")
//   - Invalid sort field
//   - Invalid sort direction
//
// GET /vms/{id} - Returns detailed VM information.
//
// Errors:
//   - 404 Not Found: VM not found
//
// # VDDK Handler
//
// POST /vddk - Uploads a VDDK tarball to the agent's data directory.
//
// The request body should contain the raw tarball data (application/octet-stream).
// Maximum file size is 64MB.
//
// Response:
//
//	{
//	    "md5": "d41d8cd98f00b204e9800998ecf8427e",  // MD5 checksum of uploaded file
//	    "bytes": 52428800                           // Number of bytes written
//	}
//
// Errors:
//   - 413 Request Entity Too Large: File exceeds 64MB limit
//   - 500 Internal Server Error: Failed to create or save file
//
// The uploaded file is saved as "vddk.tar.gz" in the agent's data directory.
//
// # Error Handling
//
// Handlers use consistent error response format:
//
//	{ "error": "error message" }
//
// HTTP Status Code Mapping:
//
//	┌─────────────────────────────┬────────┬──────────────────────────────┐
//	│ Error Type                  │ Status │ When                         │
//	├─────────────────────────────┼────────┼──────────────────────────────┤
//	│ Validation error            │ 400    │ Invalid request params       │
//	│ ResourceNotFoundError       │ 404    │ Resource doesn't exist       │
//	│ CollectionInProgressError   │ 409    │ Collection already running   │
//	│ ModeConflictError           │ 409    │ Mode change after fatal err  │
//	│ MaxBytesError               │ 413    │ Upload exceeds size limit    │
//	│ Internal error              │ 500    │ Unexpected service errors    │
//	│ Not implemented             │ 501    │ Inspector endpoints          │
//	└─────────────────────────────┴────────┴──────────────────────────────┘
//
// # Model Conversion
//
// Handlers convert between internal models and API types using extension
// functions defined in api/v1/extension.go:
//
//   - v1.NewCollectorStatus(models.CollectorStatus) → v1.CollectorStatus
//   - v1.NewVirtualMachineFromSummary(models.VirtualMachineSummary) → v1.VirtualMachine
//   - v1.NewVirtualMachineDetailFromModel(models.VM) → v1.VirtualMachineDetail
//   - v1.AgentStatus.FromModel(models.AgentStatus)
//
// # Framework
//
// The package uses the Gin web framework. Routes are auto-generated from
// the OpenAPI specification in api/v1/spec.gen.go.
package handlers
