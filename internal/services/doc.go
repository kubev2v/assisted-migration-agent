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
//   - Async work execution through work.Pipeline and Scheduler
//
// # Service Dependency Graph
//
//	Handlers (HTTP endpoints)
//	    │
//	    ▼
//	Services Layer
//	    ├── CollectorService ──► InventoryService, work.Service[CollectorStatus, CollectorResult]
//	    ├── InspectorService ──► inspectionService (Scheduler[InspectionResult], one work.Pipeline per VM; in-memory only, no Store)
//	    ├── Console ──────────► Store, work.Pipeline (creates Scheduler[any] per run loop), Console Client, Collector
//	    ├── InventoryService ─► Store
//	    ├── VMService ────────► Store
//	    └── GroupService ─────► Store
//
// # work.Service and work.Pool
//
// work.Service and work.Pool are one-time consumable executors that own a Scheduler
// and one or more work.Pipelines for their entire lifecycle. They eliminate the
// boilerplate of scheduler creation, pipeline wiring, start/stop coordination, and
// state exposure that every async service would otherwise repeat.
//
// Both are disposable: create → start → read state → discard. The coordinator
// (e.g. CollectorService) creates a new instance for each run. There is no restart.
//
// work.Service[S, R] — single builder, single pipeline, 1 worker:
//
//	srv := work.NewService(initialState, builder)
//	err := srv.Start()
//	state := srv.State()   // always valid after Start
//	srv.IsRunning()        // true while pipeline goroutine is active
//	srv.Stop()             // cancels; state persists (result/error readable)
//
// work.Pool[S, R] — multiple builders keyed by string, shared scheduler, N workers:
//
//	pool := work.NewPool(workers, entries)
//	err := pool.Start()
//	state, err := pool.State("key")  // per-key; error if key unknown
//	pool.IsRunning()                 // true if any pipeline is active
//	pool.Cancel("key")              // stops a single pipeline
//	pool.Stop()                     // stops all; state persists per key
//
// Creating a new async service:
//
// 1. Define your status type S and result type R.
//
//  2. Build a WorkBuilder[S, R] that produces WorkUnit steps — typically via
//     a factory struct that holds domain dependencies (store, credentials, etc.).
//     The factory is created by the ServiceManager and injected into the coordinator.
//
// 3. Write a coordinator service (like CollectorService) that:
//
//   - Holds precondition logic (e.g. "don't start if inventory exists")
//
//   - Creates a new work.Service or work.Pool for each run
//
//   - Exposes domain-specific GetStatus by reading the executor's State()
//
//   - Translates generic errors (ServiceAlreadyStartedError) to domain errors
//
//     4. Wire it in ServiceManager.Initialize: create the factory, pass its Build
//     method to the coordinator constructor.
//
// Example (single-pipeline coordinator):
//
//	type MyService struct {
//	    mu      sync.Mutex
//	    workSrv *work.Service[MyStatus, MyResult]
//	    buildFn func(params Params) work.WorkBuilder[MyStatus, MyResult]
//	}
//
//	func (s *MyService) Start(params Params) error {
//	    s.mu.Lock()
//	    defer s.mu.Unlock()
//	    if s.workSrv != nil && s.workSrv.IsRunning() {
//	        return ErrAlreadyRunning
//	    }
//	    s.workSrv = work.NewService(initialState, s.buildFn(params))
//	    return s.workSrv.Start()
//	}
//
// # CollectorService
//
// CollectorService manages VM inventory collection from vCenter, handling state
// transitions and asynchronous work execution.
//
// It is a coordinator over disposable work.Service instances. The domain logic
// (vCenter connection, collection, parsing) lives in a collectorWorkFactory that
// is created by ServiceManager and injected as a builder function.
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
//   - Each Start creates a new work.Service; the coordinator checks preconditions before creating it
//   - GetStatus checks the database for inventory first (authoritative for Collected),
//     then falls back to the work.Service state, then Ready
//
// Usage:
//
//	// In ServiceManager.Initialize:
//	factory := newCollectorWorkFactory(store, eventSrv, dataDir, opaPoliciesDir)
//	collector := NewCollectorService(inventorySrv, factory.Build)
//
//	// At runtime:
//	err := collector.Start(ctx, credentials)
//	status := collector.GetStatus()
//	collector.Stop()
//
// # InspectorService
//
// InspectorService drives VM inspection against vCenter: privilege validation, snapshot lifecycle,
// disk inspection via VDDK, and result persistence.
//
// ## Two-layer architecture
//
// InspectorService is split into two layers with distinct responsibilities:
//
//   - InspectorService (exported) — owns the service lifecycle: vCenter credentials,
//     vSphere client creation/teardown, scheduler, and the public API (Start/Stop/Cancel/GetStatus).
//     It holds a mutex that protects lifecycle transitions.
//
//   - inspectionService (unexported) — owns a single inspection run: the per-VM pipeline map,
//     the polling loop that detects completion, and the cleanup callback. It is a one-time
//     consumable, enforced by a buffered consumed channel (cap 1): the first Start() fills
//     the channel and proceeds; subsequent Start() calls see the channel full and return
//     immediately as no-ops. It holds its own mutex for short pipeline-map operations.
//
// Keeping them apart prevents lock nesting and lets inspectionService be tested independently
// of vSphere connection handling.
//
// ## Lifecycle
//
// The inspector has two service-level states, determined by whether inspectionSvc is nil:
//
//	┌───────┐     ┌─────────┐
//	│ Ready │────►│ Running │
//	└───────┘     └─────────┘
//	    ▲               │
//	    │               │ (all pipelines finished, or Stop() called)
//	    └───────────────┘
//
// Per-VM status lives on each WorkPipeline (pending, running, completed, error, canceled).
//
// A full inspection cycle:
//
//  1. (Optional) Caller verifies credentials via Credentials() (preflight check against vCenter).
//  2. Caller calls Start(ctx, creds, vmIDs).
//     a. Start acquires the mutex, rejects if already running (InspectionInProgressError).
//     b. Creates a new vSphere client and vmdetect.Detector.
//     c. Creates a fresh inspectionService, passing the shared scheduler, operator, and detector.
//     d. Calls inspectionService.Start(vmIDs, cleanupFn) which:
//     - Creates one WorkPipeline per VM and starts them on the scheduler.
//     - Launches a background goroutine (run) that polls IsBusy() every 5s.
//     e. The cleanup function logs out the vSphere client and nils inspectionSvc
//     on InspectorService (returning it to Ready).
//  3. Pipelines execute concurrently via the shared scheduler. Each pipeline runs
//     its work units sequentially (validate → snapshot → inspect → save → remove snapshot).
//  4. The run() goroutine detects completion (IsBusy() == false) and calls the cleanup
//     function, which logs out the vSphere client and transitions the service back to Ready.
//  5. Alternatively, Stop() can be called at any time:
//     a. It grabs inspectionSvc under lock, nils the field (immediate Ready transition),
//     then calls inspectionService.Stop() outside the lock.
//     b. inspectionService.Stop() cancels all pipelines, signals the run() goroutine
//     via the stop channel, and waits for cleanup to finish.
//
// ## Scheduler lifetime
//
// The scheduler is created once in NewInspectorService and shared across inspection runs.
// It is NOT closed on Stop — only the pipelines are torn down. This allows subsequent
// Start calls to reuse the same scheduler without recreation.
//
// ## Coordination between run() and Stop()
//
// inspectionService uses a buffered stop channel (cap 1) as a single-bit flag to coordinate
// shutdown between run() and Stop(). Because the consumed channel guarantees at most one
// run() goroutine ever exists, Stop() can never race with itself:
//
//   - Stop() sets waitCleanupCh, stops all pipelines, then does a non-blocking send on the
//     stop channel. If run() is still alive the send succeeds and Stop() waits on waitCleanupCh.
//   - run() on natural completion fills the stop channel via non-blocking send. If Stop() later
//     tries to send, the channel is full so the default branch is taken — Stop() skips waiting.
//   - The cleanup function (vSphere logout + nil inspectionSvc) runs exactly once in run()’s defer.
//
// Usage:
//
//	inspector, err := services.NewInspectorService(store, 10, dataDir)
//	err = inspector.Credentials(ctx, creds) // optional preflight check
//	err = inspector.Start(ctx, creds, []string{“vm-1”, “vm-2”})
//	status := inspector.GetStatus()       // Ready or Running
//	vmStatus := inspector.GetVmStatus(“vm-1”)  // per-VM pipeline state
//	err = inspector.Cancel(“vm-2”)        // cancel a single VM’s pipeline
//	err = inspector.Stop()                // cancel entire run, wait for cleanup
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
//   - Periodic status and inventory dispatching via a reusable work.Pipeline
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
//   - Coordinates disposable work.Service instances under sync.Mutex
//   - Each work.Service owns its own pipeline and scheduler lifecycle
//   - GetStatus reads work.Service.State() which delegates to the pipeline
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
//   - sync.Mutex protects inspectionSvc, credentials, and lifecycle transitions (Start/Stop/Cancel)
//   - GetStatus and IsBusy read inspectionSvc under lock
//   - GetVmStatus snapshots inspectionSvc under lock, then delegates without holding it
//
// inspectionService (internal):
//   - sync.Mutex protects the per-VM pipeline map; short-held locks around map reads/writes
//   - stop channel (cap 1) coordinates run() and Stop() without additional locks
//   - Pipelines torn down in Stop(); IsBusy inspects pipeline.IsRunning() under lock
package services
