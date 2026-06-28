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
//	    ├── InspectorService ──► work.Pool2 (one Pipeline2 per VM), Store
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
//	err := collector.Start(ctx)
//	status := collector.GetStatus()
//	collector.Stop()
//
// # InspectorService
//
// InspectorService drives VM inspection against vCenter: privilege validation, snapshot lifecycle,
// disk inspection via VDDK, and result persistence.
//
// ## Architecture
//
// InspectorService directly owns a work.Pool2 that manages one Pipeline2 per VM. Each pipeline
// is defined by an inspectionBuilder (implements WorkBuilder2) that yields three work units
// (validate, snapshot, inspect+save) and a Finalize method for cleanup.
//
// ## Lifecycle
//
// The inspector has two service-level states, determined by whether pool is nil:
//
//	┌───────┐     ┌─────────┐
//	│ Ready │────►│ Running │
//	└───────┘     └─────────┘
//	    ▲               │
//	    │               │ (all pipelines finished, or Stop() called)
//	    └───────────────┘
//
// Per-VM terminal status is persisted by inspectionBuilder.Finalize, which always runs
// (even on cancel/error). Finalize determines the terminal state from InspectionResult:
//   - result.Err != nil → error
//   - result.Completed == true → completed
//   - otherwise → canceled (pipeline stopped before last work unit set Completed)
//
// A full inspection cycle:
//
//  1. (Optional) Caller verifies credentials via Credentials() (preflight check against vCenter).
//  2. Caller calls Start(ctx, creds, vmIDs).
//     a. Start acquires the mutex, rejects if already running (InspectionInProgressError).
//     b. Creates a new vSphere client, vmdetect.Detector, and VMOperator.
//     c. Builds a map of WorkBuilder2 instances (one inspectionBuilder per VM).
//     d. Creates Pool2 with WithWorkers and a pool-level WithFinalizer (vClient logout + nil pool).
//     e. Starts the pool.
//  3. Pipelines execute concurrently. Each pipeline runs its work units sequentially
//     (validate → snapshot → inspect+save). The last unit sets result.Completed = true.
//  4. When all pipelines finish, the pool-level finalizer logs out the vSphere client
//     and nils the pool under mutex, transitioning the service back to Ready.
//  5. Alternatively, Stop() or Cancel(id) can be called at any time:
//     - Stop() captures the pool ref under lock, releases lock, calls pool.Stop() which
//     blocks until all per-pipeline Finalize and the pool-level finalizer complete.
//     - Cancel(id) captures the pool ref under lock, releases lock, calls pool.Cancel(id)
//     which blocks until that pipeline’s Finalize completes.
//
// ## Snapshot cleanup
//
// Snapshot removal is handled in inspectionBuilder.Finalize, which always runs regardless of
// how the pipeline ended. If result.SnapshotID is non-empty, Finalize removes the snapshot
// before persisting the terminal status.
//
// Usage:
//
//	inspector, err := services.NewInspectorService(store, 10, dataDir)
//	err = inspector.Credentials(ctx, creds) // optional preflight check
//	err = inspector.Start(ctx, []string{“vm-1”, “vm-2”})
//	status := inspector.GetStatus()       // Ready or Running
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
// # CredentialsService
//
// CredentialsService manages the agent-wide master password and per-source
// encrypted credentials (vCenter URL, username, password).
//
// It is NOT wired into ServiceManager. It is a shared dependency: services
// that need credential access (e.g. collector, inspector) should receive it
// as a constructor parameter.
//
// The master password is hashed with argon2id and stored in a dedicated
// single-row table. Credential fields (username, password) are encrypted
// with XChaCha20-Poly1305 using a key derived via argon2id from a SHA-256
// hash of the master password. Each encrypted field is self-contained:
// base64(salt || nonce || ciphertext). The URL is stored in plaintext.
//
// The raw master password is never kept in memory beyond login. After
// VerifyPassword succeeds, the caller should compute crypto.Hash256(password)
// and store the resulting []byte in the session. All subsequent Save/Get
// calls accept this SHA-256 hash directly. SetPassword handles hashing
// internally during password rotation.
//
// Operations:
//   - SetPassword: rotates the master password, re-encrypting all credentials atomically
//   - HasPassword: checks if a master password exists
//   - VerifyPassword: verifies a password against the stored argon2id hash
//   - Save: encrypts credentials with the SHA-256 hash and stores them
//   - Get: retrieves and decrypts credentials using the SHA-256 hash
//   - List: returns all credential IDs
//   - Delete: removes credentials by ID (idempotent)
//
// Usage:
//
//	credsSrv := services.NewCredentialsService(store)
//	cr := crypto.NewCrypto()
//	err := credsSrv.SetPassword(ctx, "", "master-key") // initial setup
//	hash := cr.Hash256("master-key")                   // keep in session
//	err = credsSrv.Save(ctx, hash, "vc-1", models.Credentials{
//	    URL: "https://vcenter.local/sdk", Username: "admin", Password: "secret",
//	})
//	creds, err := credsSrv.Get(ctx, hash, "vc-1")
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
//   - sync.Mutex protects pool field and lifecycle transitions (Start/Stop/Cancel)
//   - GetStatus and IsBusy check pool == nil under lock
//   - Stop/Cancel capture pool ref under lock, release it, then call blocking pool methods
//   - Pool-level finalizer acquires mutex to nil out pool (runs after all pipelines complete)
package services
