// Package store implements the data access layer for the assisted-migration-agent.
//
// This package provides persistent storage using DuckDB, combining locally-defined
// tables for agent configuration with tables created by duckdb_parser for VMware
// inventory data.
//
// # Key design decisions
//
// All sub-stores share a single DuckDB connection (single-writer model,
// MaxOpenConns=1). Transactions propagate implicitly: Store.WithTx attaches
// a sql.Tx to the context, and the shared QueryInterceptor routes all
// subsequent queries through that transaction automatically. This means
// any store method called inside a WithTx callback participates in the
// transaction without explicit tx passing. FORCE CHECKPOINT is suppressed
// inside transactions because DuckDB does not support it there.
//
// The database uses DuckDB's WAL mode. Checkpoint() forces a WAL flush to
// the main file. The sqlite_scanner extension is bundled and loaded at
// connection time so Parser().IngestSqlite() can read collector output
// directly without downloading it at runtime. This is required because the
// agent may be deployed in air-gapped environments with no internet access.
//
// # Architecture Overview
//
//	┌─────────────────────────────────────────────────────────────────────────────────────┐
//	│                                   Store (facade)                                    │
//	├─────────────────────────────────────────────────────────────────────────────────────┤
//	│  ConfigurationStore │  InventoryStore     │   GroupStore        │  InspectionStore  │
//	│         ▼           │        ▼            │       ▼             │         ▼         │
//	│   configuration     │    inventory        │    groups           │  vm_inspection_*  │
//	│      (local)        │     (local)         │    (local)          │      (local)      │
//	├─────────────────────┴─────────────────────┴──────────────────── ┴───────────────────┤
//	│                                       VMStore                                       │
//	│                                          ▼                                          │
//	│             vinfo, vdisk, concerns (duckdb_parser); joins local tables              │
//	└─────────────────────────────────────────────────────────────────────────────────────┘
//
// # Data Sources
//
// Tables created by LOCAL MIGRATIONS (internal/store/migrations/sql/):
//
//	┌─────────────────────────┬─────────────────────────────────────────────┐
//	│  Table                  │  Purpose                                    │
//	├─────────────────────────┼─────────────────────────────────────────────┤
//	│  configuration          │  Agent runtime config (agent_mode)          │
//	│  inventory              │  Raw inventory JSON blob with timestamps    │
//	│  groups                 │  Named filter expressions for VM grouping   │
//	│  group_matches          │  Pre-computed group→VM ID matches           │
//	│  schema_migrations      │  Migration version tracking                 │
//	│  vm_inspection_status   │ Per-VM deep-inspection state / queue        │
//	│  vm_inspection_concerns │ Per-run inspection concern rows (FK vinfo)  │
//	└─────────────────────────┴─────────────────────────────────────────────┘
//
// Tables created by DUCKDB_PARSER (parser.Init()):
//
//	┌─────────────────┬────────────────────────────────────────────────┐
//	│  Table          │  Purpose                                       │
//	├─────────────────┼────────────────────────────────────────────────┤
//	│  vinfo          │  Main VM info (ID, name, powerstate, cluster)  │
//	│  vcpu           │  CPU configuration per VM                      │
//	│  vmemory        │  Memory configuration per VM                   │
//	│  vdisk          │  Virtual disk info (capacity, RDM, sharing)    │
//	│  vnetwork       │  Network interfaces per VM                     │
//	│  vhost          │  ESXi host information                         │
//	│  vdatastore     │  Storage datastore information                 │
//	│  vhba           │  Host Bus Adapter information                  │
//	│  dvport         │  Distributed virtual port/VLAN info            │
//	│  concerns       │  Migration concerns/warnings per VM            │
//	└─────────────────┴────────────────────────────────────────────────┘
//
// # Initialization Flow
//
//	NewStore(db)
//	    ├── Creates duckdb_parser.Parser
//	    └── Initializes all sub-stores with QueryInterceptor
//
//	Store.Migrate(ctx)
//	    ├── parser.Init()     → Creates vinfo, vdisk, concerns, etc.
//	    └── migrations.Run()  → Creates configuration, inventory, inspection tables, …
//
// # Store Components
//
// # ConfigurationStore
//
// Persists agent runtime configuration in a single-row table.
//
// Schema:
//
//	configuration (
//	    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
//	    agent_mode VARCHAR DEFAULT 'disconnected'
//	)
//
// Methods:
//   - Get(ctx) → *models.Configuration
//   - Save(ctx, cfg) → error (uses UPSERT)
//
// # InventoryStore
//
// Stores raw inventory data as a JSON blob. Used by Console service to send
// inventory to console.redhat.com.
//
// Schema:
//
//	inventory (
//	    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
//	    data BLOB NOT NULL,
//	    created_at TIMESTAMP,
//	    updated_at TIMESTAMP
//	)
//
// Methods:
//   - Get(ctx) → *models.Inventory
//   - Save(ctx, data []byte) → error (uses UPSERT, updates updated_at)
//
// # GroupStore
//
// Stores named filter expressions (groups) that dynamically match VMs.
// Each group has a filter DSL expression and optional tags. Matching VM IDs
// are pre-computed into the group_matches table at write time so that
// reads (e.g. listing VMs for a group) avoid re-evaluating the filter DSL.
//
// Schema:
//
//	groups (
//	    id          INTEGER PRIMARY KEY DEFAULT nextval('id_sequence'),
//	    created_at  TIMESTAMP DEFAULT now(),
//	    updated_at  TIMESTAMP DEFAULT now(),
//	    name        VARCHAR NOT NULL,
//	    filter      VARCHAR NOT NULL,
//	    description VARCHAR,
//	    tags        VARCHAR[]
//	)
//
//	group_matches (
//	    group_id INTEGER PRIMARY KEY,
//	    vm_ids   VARCHAR[]
//	)
//
// CRUD Methods:
//   - List(ctx, filters []sq.Sqlizer, limit, offset uint64) → ([]models.Group, error)
//     Returns groups ordered by ID ascending. Applies optional sq.Sqlizer
//     filters (e.g. from filter.ParseWithGroupMap) and LIMIT/OFFSET pagination.
//   - Count(ctx, filters ...sq.Sqlizer) → (int, error)
//     Returns the total number of groups matching the given filters.
//   - Get(ctx, id) → *models.Group (returns ResourceNotFoundError if missing)
//   - Create(ctx, group) → *models.Group (with generated ID and timestamps)
//   - Update(ctx, id, group) → *models.Group (returns ResourceNotFoundError if missing)
//   - Delete(ctx, id) → error (returns ResourceNotFoundError if missing)
//
// Match Methods:
//   - RefreshMatches(ctx, groupIDs ...int)
//     Re-evaluates group filters and writes matching VM IDs into group_matches.
//     When groupIDs are provided only those groups are refreshed; otherwise all
//     groups are refreshed. Runs inside a WithTx transaction for atomicity.
//   - GetMatchedVMIDs(ctx, groupID int) → ([]string, error)
//     Returns the pre-computed VM IDs for a single group.
//   - DeleteMatches(ctx, groupID int) → error
//     Removes the group_matches row for a group (used on group deletion).
//
// # InspectionStore
//
// Persists deep-inspection workflow state and structured concern lines from
// inspection runs. This is separate from the duckdb_parser `concerns` table
// (inventory migration assessment / issue counts).
//
// Each call to InsertResult allocates a new inspection_id (vm_inspection_id_seq)
// and inserts one row per concern. VM list/filter joins the latest run per VM
// (max inspection_id) as alias `ic` for inspection_concern.* filter fields.
//
// Methods (status): Get, List, First, Add, Update, DeleteAll.
// Methods (concerns): InsertResult, ListResults.
//
// # VMStore
//
// Provides read access to VM inventory data. Uses a hybrid approach:
//   - List/Count: Two-step query with flat filter subquery + aggregated output
//   - Get: Uses parser.VMs() for full VM details with all relationships
//
// Query Architecture:
//
// The query is split into two steps so that the filter DSL can reference any
// raw column from any table, while the output remains one row per VM:
//
//  1. Filter step — flat JOIN of vinfo, disks, inventory concerns, inspection
//     status, inspection concerns (latest run per VM), CPU/mem/net, aggregates,
//     datastore; apply WHERE; extract DISTINCT VM IDs
//  2. Output step — aggregated query (subquery JOINs) restricted to matched IDs
//
// Filter Subquery (flat JOIN, all columns available):
//
//	SELECT DISTINCT v."VM ID"
//	FROM vinfo v
//	LEFT JOIN vdisk dk           ON v."VM ID" = dk."VM ID"
//	LEFT JOIN concerns c         ON v."VM ID" = c."VM_ID"
//	LEFT JOIN vm_inspection_status i ON v."VM ID" = i."VM ID"
//	LEFT JOIN vm_inspection_concerns ic ON v."VM ID" = ic."VM ID"
//	     AND ic.inspection_id = (SELECT MAX(inspection_id) FROM vm_inspection_concerns imx WHERE imx."VM ID" = v."VM ID")
//	LEFT JOIN vcpu cpu           ON v."VM ID" = cpu."VM ID"
//	LEFT JOIN vmemory mem        ON v."VM ID" = mem."VM ID"
//	LEFT JOIN vnetwork net       ON v."VM ID" = net."VM ID"
//	LEFT JOIN vdatastore ds      ON ds."Name" = regexp_extract(...)
//	WHERE <filter conditions>
//
// Output Query (aggregated, one row per VM):
//
//	SELECT v."VM ID" AS id, v."VM" AS name, ...
//	       COALESCE(d.total_disk, 0) AS disk_size,
//	       COALESCE(c.issue_count, 0) AS issue_count,
//	       COALESCE(t.tags, [])::VARCHAR[] AS tags
//	FROM vinfo v
//	LEFT JOIN (...disk subquery...) d  ON v."VM ID" = d."VM ID"
//	LEFT JOIN (...concern subquery...) c ON v."VM ID" = c."VM_ID"
//	LEFT JOIN (...tags subquery...) t ON v."VM ID" = t.vm_id
//	WHERE v."VM ID" IN (filter subquery)
//	ORDER BY / LIMIT / OFFSET
//
// The tags subquery derives tags for each VM by joining group_matches
// (UNNEST vm_ids) with groups (tags column) and aggregating the distinct
// tag values into a single array per VM.
//
// API:
//
// Filters are sq.Sqlizer values (WHERE clauses for the flat subquery).
// Query options are ListOption functions (sort/pagination for the output query).
// List takes both:
//
//	vms, err := store.VM().List(ctx,
//	    []sq.Sqlizer{
//	        store.ByFilter("cluster = 'production' and memory >= 8GB"),
//	    },
//	    store.WithSort([]store.SortParam{{Field: "name", Desc: false}}),
//	    store.WithLimit(50),
//	)
//
// Count takes only filters:
//
//	total, err := store.VM().Count(ctx, store.ByFilter("status = 'poweredOn'"))
//
// Filtering:
//
//   - ByFilter(expr string)
//     Parses a filter DSL expression (see pkg/filter) into a sq.Sqlizer.
//     The DSL can reference columns from the flat filter join using
//     dot-notation prefixes: disk.*, concern.* (inventory), inspection.*,
//     cpu.*, mem.*, net.*, datastore.*, plus all flat vinfo columns.
//
// Pagination & Filtering Options (ListOption):
//
//   - WithLimit(limit uint64)
//   - WithOffset(offset uint64)
//   - WithVMIDs(ids []string) — restricts output to an explicit set of VM IDs
//     (used by GroupService.ListVirtualMachines to avoid filter re-evaluation)
//
// Sorting Options (ListOption):
//
//   - WithSort(sorts []SortParam)
//     Applies multi-field sorting using output aliases.
//     Always appends "id" as tie-breaker for stable sorting.
//
//   - WithDefaultSort()
//     Sorts by id ascending.
//
// Sort Field Mapping (API field -> output alias):
//
//	┌──────────────┬──────────────┐
//	│  API Field   │  Alias       │
//	├──────────────┼──────────────┤
//	│  name        │  name        │
//	│  vCenterState│  power_state │
//	│  cluster     │  cluster     │
//	│  diskSize    │  disk_size   │
//	│  memory      │  memory      │
//	│  issues      │  issue_count │
//	└──────────────┴──────────────┘
//
// # QueryInterceptor
//
// All database operations are wrapped with a QueryInterceptor that provides
// debug logging for all queries. This enables visibility into SQL execution
// without modifying individual store implementations.
//
// The interceptor is transaction-aware: when the context carries an active
// *sql.Tx (injected by WithTx), all three methods route queries through
// the transaction instead of the raw *sql.DB. The interceptor also suppresses
// FORCE CHECKPOINT inside a transaction to avoid DuckDB errors.
//
// Logged operations:
//   - QueryRowContext
//   - QueryContext
//   - ExecContext
//
// # Transactions (WithTx) — Inverted Transaction Control
//
// Store uses an inverted transaction pattern: the transaction is not passed
// explicitly to each store method. Instead, Store.WithTx attaches the sql.Tx
// to the context, and the QueryInterceptor transparently routes all subsequent
// queries through that transaction. Store methods don't need to know whether
// they are running inside a transaction or not — they just use the context
// they receive.
//
// This inversion matters because it keeps store methods simple. A method like
// GroupStore.Create works identically whether called standalone or inside a
// WithTx block. The caller at the service layer decides transactional scope;
// the store layer executes queries without caring.
//
// How it works:
//
//  1. Store.WithTx(ctx, fn) calls db.BeginTx and attaches the *sql.Tx to a
//     new context via context.WithValue using a private key (txKey).
//  2. The fn callback receives this enriched context.
//  3. When any store method calls QueryInterceptor.ExecContext (or
//     QueryContext, QueryRowContext), the interceptor checks the context
//     for an active tx. If found, it routes the query through the tx.
//     If not, it routes through the raw *sql.DB.
//  4. On success, WithTx commits. On error (returned from fn), it rolls back.
//
// Usage at the service layer:
//
//	// Atomic create: insert group row + compute matching VMs in one tx.
//	err := s.store.WithTx(ctx, func(txCtx context.Context) error {
//	    created, err := s.store.Group().Create(txCtx, group)
//	    if err != nil { return err }
//	    return s.store.Group().RefreshMatches(txCtx, created.ID)
//	})
//
//	// Atomic delete: remove matches + delete group in one tx.
//	err := s.store.WithTx(ctx, func(txCtx context.Context) error {
//	    if err := s.store.Group().Delete(txCtx, id); err != nil {
//	        return err
//	    }
//	    return s.store.Group().DeleteMatches(txCtx, id)
//	})
//
// The key rule: always pass txCtx (the context from the WithTx callback)
// to store methods inside the block. Passing the original ctx instead
// bypasses the transaction silently.
//
// Side effects inside transactions:
//
// The QueryInterceptor suppresses FORCE CHECKPOINT when a transaction is
// active, because DuckDB does not support checkpointing inside a
// transaction. Outside a transaction, ExecContext automatically runs
// FORCE CHECKPOINT after every write to flush the WAL.
//
// Nesting:
//
// Nested WithTx calls are not supported. A second WithTx inside an
// existing transaction will start an independent transaction on the raw
// *sql.DB, which defeats atomicity. Services should structure their code
// so that a single WithTx block wraps all related writes.
//
// # Design Patterns
//
// Single-Row Tables:
//   - Configuration and Inventory use CHECK (id = 1) constraint
//   - Guarantees only one record per logical entity
//   - Uses UPSERT pattern: INSERT ... ON CONFLICT (id) DO UPDATE
//
// Functional Options:
//   - VMStore uses ListOption functions for composable query building
//   - Each option modifies a squirrel.SelectBuilder
//   - Options can be combined for complex queries
//
// Separation of Concerns:
//   - Local tables: Agent state (configuration, raw inventory, groups,
//     vm_inspection_status, vm_inspection_concerns, …)
//   - Parser tables: Structured VMware inventory (VMs, hosts, datastores)
//   - VMStore bridges both: queries parser tables, returns domain models
package store
