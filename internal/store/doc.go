// Package store implements the data access layer for the assisted-migration-agent.
//
// This package provides persistent storage using DuckDB, combining locally-defined
// tables for agent configuration with tables created by duckdb_parser for VMware
// inventory data.
//
// # Architecture Overview
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                         Store (facade)                          │
//	├─────────────────────────────────────────────────────────────────┤
//	│  ConfigurationStore  │  InventoryStore  │   GroupStore        │
//	│         ▼            │        ▼          │       ▼             │
//	│   configuration      │    inventory      │    groups           │
//	│      (local)         │     (local)       │    (local)          │
//	├──────────────────────┴──────────────────┴─────────────────────┤
//	│                          VMStore                               │
//	│                             ▼                                  │
//	│    vinfo, vdisk, concerns (from duckdb_parser)                 │
//	└────────────────────────────────────────────────────────────────┘
//
// # Data Sources
//
// Tables created by LOCAL MIGRATIONS (internal/store/migrations/sql/):
//
//	┌────────────────────┬─────────────────────────────────────────────┐
//	│  Table             │  Purpose                                    │
//	├────────────────────┼─────────────────────────────────────────────┤
//	│  configuration     │  Agent runtime config (agent_mode)          │
//	│  inventory         │  Raw inventory JSON blob with timestamps    │
//	│  groups            │  Named filter expressions for VM grouping   │
//	│  schema_migrations │  Migration version tracking                 │
//	└────────────────────┴─────────────────────────────────────────────┘
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
//	    └── migrations.Run()  → Creates configuration, inventory
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
// Each group has a filter DSL expression that is evaluated at query time
// against the VM table.
//
// Schema:
//
//	groups (
//	    id          INTEGER PRIMARY KEY DEFAULT nextval('id_sequence'),
//	    created_at  TIMESTAMP DEFAULT now(),
//	    updated_at  TIMESTAMP DEFAULT now(),
//	    name        VARCHAR NOT NULL,
//	    filter      VARCHAR NOT NULL,
//	    description VARCHAR
//	)
//
// Methods:
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
//  1. Filter step — flat JOIN of all 8 tables, apply WHERE, extract DISTINCT VM IDs
//  2. Output step — aggregated query (subquery JOINs) restricted to matched IDs
//
// Filter Subquery (flat JOIN, all columns available):
//
//	SELECT DISTINCT v."VM ID"
//	FROM vinfo v
//	LEFT JOIN vdisk dk           ON v."VM ID" = dk."VM ID"
//	LEFT JOIN concerns c         ON v."VM ID" = c."VM_ID"
//	LEFT JOIN vm_inspection_status i ON v."VM ID" = i."VM ID"
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
//	       COALESCE(c.issue_count, 0) AS issue_count
//	FROM vinfo v
//	LEFT JOIN (...disk subquery...) d  ON v."VM ID" = d."VM ID"
//	LEFT JOIN (...concern subquery...) c ON v."VM ID" = c."VM_ID"
//	WHERE v."VM ID" IN (filter subquery)
//	ORDER BY / LIMIT / OFFSET
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
//     The DSL can reference any column from all 8 joined tables using
//     dot-notation prefixes: disk.*, concern.*, cpu.*, mem.*, net.*, datastore.*,
//     inspection.*, plus all flat vinfo columns.
//
// Pagination Options (ListOption):
//
//   - WithLimit(limit uint64)
//   - WithOffset(offset uint64)
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
// Logged operations:
//   - QueryRowContext
//   - QueryContext
//   - ExecContext
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
//   - Local tables: Agent state (configuration, raw inventory)
//   - Parser tables: Structured VMware inventory (VMs, hosts, datastores)
//   - VMStore bridges both: queries parser tables, returns domain models
package store
