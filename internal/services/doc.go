// Package services implements the business logic layer for the assisted-migration-agent.
//
// This package contains services that act as intermediaries between HTTP handlers
// and the data store, providing a clean separation of concerns. Each service
// encapsulates specific domain logic and manages its own state where applicable.
//
// # Architecture Overview
//
// The services layer follows these design principles:
//   - Interface-based dependencies for testability
//   - Mutex-protected state for thread safety
//   - Channel-based signaling for goroutine coordination
//   - Async work execution through WorkPipeline and Scheduler
//
// # Service Dependency Graph
//
//	Handlers (HTTP endpoints)
//	    │
//	    ▼
//	Services Layer
//	    ├── CollectorService ──► Store, WorkPipeline (owns its own Scheduler[CollectorResult])
//	    ├── InspectorService ──► inspectionService (Scheduler[InspectionResult], one WorkPipeline per VM; in-memory only, no Store)
//	    ├── Console ──────────► Store, WorkPipeline (creates Scheduler[any] per run loop), Console Client, Collector
//	    ├── InventoryService ─► Store
//	    ├── VMService ────────► Store
//	    └── GroupService ─────► Store
//
// # CollectorService
//
// CollectorService manages VM inventory collection from vCenter, handling state
// transitions and asynchronous work execution.
//
// State Machine:
//
//	┌───────┐    ┌────────────┐    ┌────────────┐    ┌─────────┐    ┌───────────┐
//	│ Ready │───►│ Connecting │───►│ Collecting │───►│ Parsing │───►│ Collected │
//	└───────┘    └────────────┘    └────────────┘    └─────────┘    └───────────┘
//	    ▲              │                 │                │          (terminal)
//	    │              │                 │                │
//	    │   (cancel)   │     (cancel)    │    (cancel)    │
//	    ├──────────────┴─────────────────┴────────────────┤
//	    │                                                 │
//	    │              │                 │                │
//	    │              ▼                 ▼                ▼
//	    │         ┌────────────────────────────────────────────┐
//	    └─────────│                   Error                    │
//	   (restart)  └────────────────────────────────────────────┘
//
// States:
//   - Ready: Initial state, waiting for collection request
//   - Connecting: Verifying vCenter credentials
//   - Collecting: Inventory collection in progress
//   - Parsing: Ingesting collected data into DuckDB, building inventory
//   - Collected: Collection completed successfully (terminal state, no way back)
//   - Error: An error occurred during operation (can restart from here)
//
// Key behaviors:
//   - Only one collection can be in progress at a time (returns CollectionInProgressError otherwise)
//   - Once inventory is collected, the Collected state is terminal - subsequent Start calls are no-ops
//   - Collection can be cancelled mid-execution via Stop, returning to Ready state
//   - Work units are executed sequentially via a callback-free WorkPipeline (see pipeline.go)
//   - The pipeline is pull-based: GetStatus reads pipeline.State() rather than receiving callbacks
//   - The collector tolerates a stale pipeline — completed pipelines remain attached until
//     replaced by a new Start() or detached by Stop()
//   - GetStatus checks the database for inventory first (authoritative for Collected),
//     then falls back to the pipeline state, then Ready
//
// Usage:
//
//	collector := services.NewCollectorService(store, dataDir, opaPoliciesDir)
//	err := collector.Start(ctx, credentials)
//	status := collector.GetStatus()
//	collector.Stop() // Cancel if needed
//
// # InspectorService
//
// InspectorService drives VM inspection against vCenter: privilege validation, snapshot lifecycle,
// and inspection and persistence steps.
//
// Per-VM inspection status is held only in memory (inspectionService pipelines).
// Restarting the agent clears all inspection state.
//
// Internal coordination uses inspectionService (unexported): a shared scheduler and one
// WorkPipeline per VM. Default work units are validate → create snapshot → inspect → save →
// remove snapshot; tests may replace the builder via WithInspectionBuilder.
//
// inspectionService exists as a separate layer because InspectorService and per-VM pipeline
// management have different responsibilities and concurrency boundaries. InspectorService
// owns the service lifecycle — vSphere client, run loop, service-level state machine — while
// inspectionService owns concurrent pipeline coordination: the shared scheduler, the per-VM
// pipeline map, and its own mutex for short-held map operations. Keeping them apart lets each
// layer manage its own lock without nesting, and lets inspectionService be tested and reasoned
// about independently of vSphere connection handling.
//
// State machine (service-level, models.InspectorState — matches HTTP inspector status;
// there is no per-VM state machine, per-VM status is derived from WorkPipeline state):
//
//	┌───────┐     ┌────────────┐     ┌─────────┐     ┌───────────┐
//	│ Ready │────►│ Initiating │────►│ Running │────►│ Completed │
//	└───────┘     └────────────┘     └─────────┘     └───────────┘
//	    ▲               │                 │           (terminal; all pipelines finished)
//	    │               │                 │
//	    │               │                 └──► Stop() ──► run loop exits ──► Canceled (terminal)
//	    │               │                 │
//	    │               │                 └──► per-VM work errors: GetVmStatus shows error; service still reaches Completed
//	    │               │
//	    │               │ (vSphere connect or pipeline start failure)
//	    │               ▼
//	    │         ┌───────────┐
//	    └─────────│   Error   │ (terminal; GetStatus().Error set)
//	              └───────────┘
//
// States:
//   - Ready: Idle, accepts Start
//   - Initiating: Connecting to vSphere and starting initial pipelines (Start holds lock for this phase)
//   - Running: Background run loop polls until no pipeline is busy, then exits to Completed, or handles Stop
//   - Error: Init failed (vSphere connect or inspectionSvc.start); error is stored on InspectorStatus
//   - Completed: Normal terminal state when all VM pipelines finish without Stop
//   - Canceled: Terminal state after Stop() once pipelines have stopped (no intermediate “canceling” state in GetStatus)
//
// All terminal states (Completed, Canceled, Error) accept a new Start() call (IsBusy returns false).
//
// Key behaviors:
//   - Only one inspection run at a time (InspectionInProgressError if already busy)
//   - Start failure during init sets Error, populates GetStatus().Error, tears down client/pipelines, and returns the error
//   - Start connects to vCenter, then starts a pipeline per initial VM ID; Add enqueues additional VMs while running
//   - Stop tears down pipelines and signals the run loop, which ends in Canceled. Cancel stops a single VM’s pipeline
//   - GetStatus reads InspectorState (separate mutex); GetVmStatus reads the corresponding WorkPipeline state
//   - run uses a ticker to detect when all per-VM pipelines have finished, then logs out the vSphere client
//
// Usage:
//
//	inspector := services.NewInspectorService(10)
//	err := inspector.Start(ctx, []string{"vm-1", "vm-2"}, credentials)
//	status := inspector.GetStatus()
//	vmStatus := inspector.GetVmStatus("vm-1")
//	err = inspector.Add("vm-3")       // optional, while busy
//	err = inspector.Cancel("vm-2")    // optional, single VM
//	err = inspector.Stop()            // cancel entire run
//
// # Console
//
// Console manages communication with the remote console server (console.redhat.com),
// periodically sending agent status and inventory updates.
//
// Agent Mode Initialization:
//
// On startup, the service determines the agent mode using the following priority:
//  1. Read agent_mode from database (configuration table)
//  2. If not present in database, use the mode from config (constructor parameter)
//  3. If config mode is invalid, default to "disconnected"
//
// If the resolved mode is "connected", the run loop starts automatically.
//
// Agent Modes:
//   - Connected: Agent actively sends status and inventory updates to console.redhat.com
//     on a configurable interval. The run loop is active and dispatching data.
//   - Disconnected: Agent does not communicate with the console. The run loop is stopped.
//     The agent operates in standalone mode, only serving local API requests.
//
// Mode Switching:
//
// The mode can be changed at runtime via SetMode(ctx, mode):
//   - Disconnected → Connected: Saves mode to database, starts the run loop
//   - Connected → Disconnected: Saves mode to database, stops the run loop
//   - Same mode: No-op (returns immediately)
//   - After fatal error (4xx): Mode changes are blocked with ModeConflictError
//
// The mode is persisted to the database so it survives agent restarts.
//
// The service implements:
//   - Periodic status and inventory dispatching via a reusable WorkPipeline
//   - SHA256 hash-based deduplication to avoid sending unchanged inventory
//   - Two-phase run loop: process result → wait (with backoff) → restart pipeline.
//     Retries fire after the backoff interval, not before it.
//   - Exponential backoff (up to 60s) for transient errors (5xx, network issues)
//   - Immediate termination on fatal errors (4xx client errors)
//   - Legacy status mode compatibility for older console versions
//
// Data sent to console:
//
// On each dispatch cycle, two API calls are made:
//
// 1. Agent Status (PUT /api/v1/agents/{id}/status):
//
//	{
//	    "credentialUrl": "http://10.10.10.1:3443",  // deprecated, will be removed
//	    "status": "collected",           // collector state: ready|connecting|collecting|collected
//	    "statusInfo": "collected",
//	    "sourceId": "uuid",
//	    "version": "1.0.0"
//	}
//
// 2. Source Inventory (PUT /api/v1/sources/{id}/status) - only if inventory changed:
//
//	{
//	    "agentId": "uuid",
//	    "inventory": {
//	        "vcenter": { ... },
//	        "infra": { "datastores": [...], "networks": [...], ... },
//	        "vms": [ { "name": "vm1", "cluster": "cluster1", ... }, ... ]
//	    }
//	}
//
// Legacy Status Mode:
//
// When legacyStatusEnabled is true, the collector states are mapped to legacy
// status values for compatibility with v1 agent version:
//
//	┌─────────────────────────────────────────────────────────┐
//	|  Current State    |  Legacy Status                      |
//	├───────────────────┼─────────────────────────────────────┤
//	|  Ready            |  waiting-for-credentials            |
//	|  Connecting       |  collecting                         |
//	|  Collecting       |  collecting                         |
//	|  Parsing          |  collecting                         |
//	|  Collected        |  collected                          |
//	└─────────────────────────────────────────────────────────┘
//
// Error handling:
//   - Transient errors: Logged, stored in status.Error, loop continues with backoff
//   - Fatal errors (4xx): Sets fatalStopped flag, exits run loop permanently
//   - Mode changes blocked after fatal stop to prevent retry loops
//
// Shutdown protocol:
//
// Stop() and SetMode(disconnected) use a non-blocking send on the close channel.
// If run() is alive, the send succeeds and a normal handshake follows. If run()
// already exited (fatal error, Start failure), the buffer contains an ack from
// run()'s deferred cleanup; the non-blocking send falls through to default and
// drains the existing ack. This prevents deadlocks regardless of how run() exited.
//
// Usage:
//
//	console := services.NewConsoleService(cfg, client, collector, store)
//	mode, err := console.GetMode(ctx)
//	err = console.SetMode(ctx, models.AgentModeConnected)
//	status := console.Status()
//
// # InventoryService
//
// InventoryService provides read-only access to collected inventory data.
// This is a lightweight stateless service that acts as a facade over the store layer.
//
// Usage:
//
//	inventoryService := services.NewInventoryService(store)
//	inventory, err := inventoryService.GetInventory(ctx)
//
// # VMService
//
// VMService manages querying and filtering virtual machines from the collected inventory.
// It supports expression-based filtering, multi-field sorting, and pagination.
//
// Filtering:
//   - A single filter DSL expression (byExpression) that can reference any column
//     across all joined tables (vinfo, vdisk, concerns, vcpu, vmemory, vnetwork,
//     vdatastore, vm_inspection_status). See pkg/filter for the grammar and field mappings.
//
// Sorting:
//   - Multiple sort fields with direction control (ascending/descending)
//   - Default sort applied when no explicit sort specified
//   - Valid fields: name, vCenterState, cluster, diskSize, memory, issues
//
// Usage:
//
//	vmService := services.NewVMService(store)
//	vm, err := vmService.Get(ctx, "vm-123")
//
//	params := services.VMListParams{
//	    Expression: "cluster = 'production' and memory >= 8GB",
//	    Sort:       []services.SortField{{Field: "name", Desc: false}},
//	    Limit:      50,
//	    Offset:     0,
//	}
//	vms, total, err := vmService.List(ctx, params)
//
// # GroupService
//
// GroupService manages CRUD operations for groups. A group is a named filter
// expression (with optional tags) that dynamically matches VMs from the
// collected inventory.
//
// Matching VM IDs are pre-computed into the group_matches table at write time,
// so reads never re-evaluate the filter DSL. Tags from matching groups are
// surfaced on VMs returned by GET /vms.
//
// Write operations (Create, Update, Delete) run inside a store.WithTx
// transaction to ensure the group row and its group_matches are updated
// atomically:
//
//	Create → store.Group().Create + RefreshMatches(groupID)
//	Update → store.Group().Update + RefreshMatches(groupID)
//	Delete → store.Group().DeleteMatches(groupID) + store.Group().Delete
//
// Operations:
//   - List: returns groups with optional name filtering and pagination.
//     Accepts GroupListParams with ByName, Limit, Offset. The ByName field
//     is converted to a filter DSL expression (name = '<value>') and parsed
//     through filter.ParseWithGroupMap to produce a sq.Sqlizer for the store.
//     Returns ([]models.Group, total int, error).
//   - Get: returns a single group by ID
//   - ListVirtualMachines: reads pre-computed VM IDs from group_matches
//     and fetches VMs by ID with sorting and pagination support
//   - Create: creates a new group and refreshes its matches (transactional)
//   - Update: updates an existing group and refreshes its matches (transactional)
//   - Delete: deletes a group and its matches (transactional)
//
// Usage:
//
//	groupService := services.NewGroupService(store)
//
//	params := services.GroupListParams{
//	    ByName: "production",
//	    Limit:  20,
//	    Offset: 0,
//	}
//	groups, total, err := groupService.List(ctx, params)
//
//	getParams := services.GroupGetParams{
//	    Sort:   []services.SortField{{Field: "name", Desc: false}},
//	    Limit:  20,
//	    Offset: 0,
//	}
//	vms, total, err := groupService.ListVirtualMachines(ctx, groupID, getParams)
//
// # Thread Safety
//
// CollectorService:
//   - Pipeline and scheduler lifecycle protected by sync.Mutex
//   - Pipeline state is pull-based (no callbacks crossing lock boundaries)
//   - Single pipeline at a time; stale completed pipelines tolerated
//
// Console:
//   - Mode changes protected by sync.Mutex (prevents double run loop)
//   - consoleState has its own separate mutex for status reads/writes,
//     preventing deadlocks between the run loop and mode changes
//   - Shutdown uses non-blocking channel send to handle self-exit safely
//
// InventoryService, VMService, and GroupService:
//   - Stateless (only hold store reference)
//   - Thread-safe through underlying store implementation
//
// InspectorService:
//   - sync.Mutex protects Start, Stop, Add, Cancel and shared fields (client, credentials, stop channel lifecycle)
//   - InspectorState uses its own mutex for GetStatus / state transitions from the run loop
//   - GetVmStatus delegates to inspectionService without holding the service mutex
//
// inspectionService (internal):
//   - sync.Mutex protects the per-VM pipeline map and stop(); short-held locks around map reads/writes
//   - Scheduler and pipelines torn down in stop(); isBusy inspects pipeline.IsRunning() under lock
package services
