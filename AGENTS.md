# Agents

Instruction for coding agents working working on assisted-migration-agent.

## Project Layout

```text
agent/
  main.go      Agent CLI entry point
  cmd/         Agent CLI commands
  app/         Agent application composition
  api/v2/      Agent OpenAPI spec and generated API types/routes
  handlers/v2/ Agent HTTP handlers, including RVTools mode
  Containerfile
  Containerfile.rvtools
analyzer/
  main.go      analyzer CLI entry point
  cmd/         analyzer CLI commands
  app/         analyzer application composition
  api/v1/      analyzer OpenAPI spec and generated API types/routes
  handlers/v1/ analyzer HTTP handlers, including RVTools mode
  Containerfile
  Containerfile.rvtools
internal/
  config/      Configuration
  handlers/    Shared validator registration
  models/      Data models (including WorkUnit, WorkBuilder)
  services/    Business logic and async orchestration
  store/       Data persistence (DuckDB)
pkg/
  console/     Console API client
  crypto/      Password hashing (argon2id) and field-level encryption (XChaCha20-Poly1305)
  errors/      Custom error types
  filter/      DSL for filtering resources (lexer, parser, SQL generator)
  logger/      Logging
  scheduler/   Generic work scheduling primitive
  work/        One-time consumable executors (Pipeline, Service, Pool)
```

- `internal/` is private to this module.
- `pkg/` holds reusable packages that can be shared outside this repository.
- Services are long-lived, stateful components wired through `ServiceManager`.

## Code Rules

- Use noun-based exported service names such as `CollectorService` and `InspectorService`.
- Keep internal coordinators and helper types unexported, such as `inspectionService` and `consoleState`.
- Name constructors `NewX`, for example `NewCollectorService()` or `work.NewPipeline()`.
- Use short receiver names that match the type, for example `func (c *Console)`.
- Use type aliases only when they materially reduce noise, for example `type consoleWorkUnit = models.WorkUnit[string, any]`.
- Add newlines between logical parts of the code. 
- Do not use inline if.
- Do not use functions as args for inverse controll pattern unless it is really needed. Ask for explicit approval.
```
Don't:
func extractName(name string, func (string) string) string
```
This pattern is useful only between packages where you want to hide an implementation from the caller. 
Using this in the same package is just noise.

- Do not create small functions just to extract a simple for loop. Keep code together. 
- Do not scatter small helper functions between unrelated public methods. A reader scanning a file should see the public API first, then the internal details — not an interleaved mix. When a file grows large enough that grouping becomes unclear, split by concern into separate files rather than adding section comments.
- Do not extract single-use logic into its own function. If a piece of code is called exactly once, it belongs inline in the caller — not wrapped in a named function that forces the reader to jump elsewhere to understand a linear flow. Extract a function only when it is called from multiple sites or when it genuinely simplifies a complex block (e.g. a deeply nested callback). A five-line helper that exists to "give a name" to something the caller already makes obvious is noise, not abstraction.
- Do not return anonimous types like `func whatever() struct { Name string }`
- If the argument or the return type is a multiple combination of map and slices like `map[string]map[string][]string`. Either use type alias or create a new type:
```
type MyType map[string]map[string][]string
or

type MyType = map[string]map[string][]string
```
- Prefer self-explanatory code. Add comments only when they explain why a constraint exists, why a lock is needed, or why a workaround is safe. 
- Avoid comments that restate the code.

## Code

### Imports

Group imports into three blocks separated by blank lines: standard library, third-party packages, then this module's packages.

```go
import (
    "context"
    "sync"

    "go.uber.org/zap"

    "github.com/kubev2v/assisted-migration-agent/internal/models"
    "github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)
```

### File Organization

Within a `.go` file, order declarations top-down by visibility and role:

1. Exported type definition and constructor (`NewX`)
2. Exported methods, grouped by concern
3. Unexported methods and helpers, grouped by concern

### OpenAPI is the source of truth

Every HTTP endpoint must be defined in the OpenAPI spec. Do not add handler-only routes.

Use the versioned spec as the source of truth:

- `agent/api/v2/openapi.yaml`

Those specs define:

- paths and HTTP methods
- request and response schemas
- status codes
- validation rules, including `x-oapi-codegen-extra-tags`

The generated files in `agent/api/v2/types.gen.go` and `agent/api/v2/spec.gen.go` are derived artifacts. Do not edit them by hand. Update `agent/api/v2/openapi.yaml`, then run `make generate`.

#### REST semantics matter

REST semantics are important in this repository. Design endpoints around resources and state transitions, not RPC-style verbs in the path.

- Use nouns for resource paths, for example `/groups/{id}`.
- Use `GET` for reads, `POST` to create or start server-side work, `PUT` or `PATCH` for updates, and `DELETE` to remove or stop a resource.
- Use path parameters for resource identity and query parameters for filtering, sorting, and pagination.
- Return status codes that match the behavior: `200` for successful reads or updates with a body, `202` for async work that has started, `204` for successful delete or stop operations with no body, `400` for invalid requests, `404` for missing resources, `409` for conflicts, and `500` for unexpected internal errors.
- For long-running workflows, start the work with one endpoint and expose progress through a separate status endpoint instead of blocking the request.

Follow the existing collector and inspector patterns: `POST` starts async work, `GET` returns current status, and `DELETE` stops it.

## Handlers

Handlers are a thin validation layer between HTTP and the service layer. They contain zero business logic. A handler's job is:

1. Parse and validate request parameters (query params, path params, request body).
2. Return `400 Bad Request` immediately if validation fails.
3. Call the appropriate service method.
4. Map the service result or error to the correct HTTP status code and response body.

If an endpoint accepts a filter expression, the handler must parse it before calling the service to catch syntax errors early:

```go
if params.ByExpression != nil {
    if _, err := filter.ParseWithDefaultMap([]byte(*params.ByExpression)); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("expression filter is invalid: %v", err)})
        return
    }
    svcParams.Expression = *params.ByExpression
}
```

## Services

Services implement the business logic layer. They sit between handlers and the store, owning domain rules and models, state transitions, and cross-store coordination.

`ServiceManager` is the central dependency injector. It creates all service instances in dependency order inside `Initialize()`, wiring cross-dependencies (e.g. `CollectorService` depends on `InventoryService`). Handlers receive services through `ServiceManager` getter methods.

Services wired through `ServiceManager`. Not every service belongs in the manager. `CredentialsService`, for example, is a shared dependency used by other services to get and save encrypted credentials — it does not respond to any request directly. It is created by its consumers, not by `ServiceManager`. See `internal/services/doc.go` for details.

Do not use raw sql queries in services. Update or create a store if you need new queries in services.

Services accept and return only domain models.

- `internal/services/doc.go` explains service ownership, state machines, and thread-safety guarantees.
- `docs/scheduler-and-pipelines.md` explains the scheduler/pipeline split and the rationale behind it.

### Async Service Design

Some services have long running task and they use primities defined in `pkg/work/`. 

Some services have long-running tasks and use primitives defined in `pkg/work/`.
Model async workflows as `work.WorkUnit` steps and run them with `work.Service` or `work.Pool`.
Use `work.Pipeline` directly only when the caller must manage the `scheduler.Scheduler` itself. When the logic cannot be modeled with these primitives, use a raw goroutine only with explicit permission.

#### Why async services use pipelines

The agent runs multi-step vCenter operations such as connecting, collecting inventory, inspecting VMs, taking snapshots, and reading disks. These operations can take seconds or minutes, must not block the HTTP API, and need to expose progress and terminal errors.

To keep that behavior consistent, every async workflow is modeled as a pipeline of work units executed through a bounded scheduler. Avoid ad-hoc goroutines and channel graphs for feature code.

#### Core building blocks

All three executors below live in `pkg/work/` and are **one-time consumable**: create → start → read state → discard. Once started, the instance cannot be restarted. Once stopped or completed, state (result and error) remains readable for the lifetime of the object. The caller creates a fresh instance for every new run.

**`Scheduler[R]`** (`pkg/scheduler/`) controls concurrency. It owns goroutine lifecycles and shared execution limits, but it does not know anything about workflow ordering.

**`work.Pipeline[S, R]`** is the low-level building block. It runs a sequence of WorkUnit steps through an externally provided Scheduler, threads the result from step to step, and stops on the first error. Pipeline does not own a scheduler — the caller creates and closes it. Use Pipeline directly only when you need to manage the scheduler yourself (e.g. Console's run loop).

```go
p := work.NewPipeline(initialState, sched, builder)
p.Start()      // start the background run
p.State()      // read current state, result, and error
p.IsRunning()  // check whether the run is still active
p.Stop()       // signal and wait until it is fully stopped
```

**`work.Service[S, R]`** wraps a single Pipeline with its own Scheduler (1 worker). It exists for the common case: one builder, one sequential pipeline, no shared concurrency budget. The coordinator (e.g. CollectorService) creates a new Service for each run.

```go
srv := work.NewService(initialState, builder)
srv.Start()     // creates scheduler + pipeline internally
srv.State()     // always valid after Start
srv.IsRunning() // true while the pipeline goroutine is active
srv.Stop()      // cancels; state persists afterward
```

**`work.Pool[S, R]`** wraps multiple Pipelines keyed by string, sharing one Scheduler with a configurable number of workers. It exists for the case where several independent work streams run concurrently against a shared worker budget (e.g. one pipeline per VM during inspection).

```go
pool := work.NewPool(workers, entries)
pool.Start()          // creates scheduler, starts all pipelines
pool.State("key")     // per-key status; error if key unknown
pool.Cancel("key")    // stops a single pipeline
pool.IsRunning()      // true if any pipeline is active
pool.Stop()           // stops all; state persists per key
```

**`WorkUnit[S, R]`** (`internal/models/work.go`) is one pipeline step:

```go
type WorkUnit[S any, R any] struct {
    Status func() S
    Work   func(ctx context.Context, result R) (R, error)
}
```

#### Service ownership

Keep ownership boundaries clear:

- The coordinator service owns preconditions, external clients, and lifecycle decisions. It creates disposable `work.Service` or `work.Pool` instances for each run.
- Domain logic for building work units (vCenter connection, collection, parsing) belongs in a factory struct, created by `ServiceManager` and injected into the coordinator.
- A `work.Service` owns one run's scheduler and pipeline. A `work.Pool` owns one run's scheduler and multiple pipelines.
- A shared `Scheduler[R]` at the service level is only needed when using `work.Pipeline` directly (e.g. Console).

`CollectorService` is the reference example for a single-pipeline coordinator:

- `ServiceManager` creates a `collectorWorkFactory` with domain dependencies and injects `factory.Build` into the collector.
- `CollectorService.Start` checks preconditions, creates a new `work.Service`, and starts it.
- `CollectorService.GetStatus` reads state from the current `work.Service`.

When a workflow needs many pipelines at once, use an unexported coordinator to manage the pipeline map. `InspectorService` and `inspectionService` are the reference example:

- `InspectorService` owns credentials, vSphere client lifecycle, and start/stop entry points.
- `inspectionService` owns the per-VM pipelines and shared scheduler.

Treat coordinators as single-use. Create fresh instances for each run instead of trying to reset and reuse old state.

#### Locking

Keep critical sections short and simple. Prefer a single lock acquisition per method over multiple lock/unlock pairs. If a method needs to do expensive work (network I/O, disk) between guarded checks, restructure so that validation and state mutation each happen under one lock acquisition, with the expensive work in between:

```go
func (s *Service) Start(ctx context.Context) error {
    // 1. Expensive work outside the lock
    client, err := connect(ctx)
    if err != nil {
        return err
    }

    // 2. One lock acquisition for state check + mutation
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.running {
        _ = client.Close()
        return ErrAlreadyRunning
    }
    s.client = client
    s.running = true
    return nil
}
```

Rules:
- Do not acquire and release the same mutex multiple times in a single method to "optimistically" check state before doing work. It creates TOCTOU gaps and makes the locking protocol harder to follow.
- Do not reach for a mutex by default. Most data does not need one. If the data flow is designed correctly — inputs resolved before work starts, passed as values, not stored as shared mutable state — there is nothing to protect. A mutex on data that isn't actually concurrent is a sign that the ownership model wasn't thought through.
- When a mutex is genuinely needed, do not protect unrelated concerns with the same one. Configuration state (credentials, settings) and run lifecycle state (active service, running flag) have different access patterns and different lifetimes. Sharing a mutex between them forces every access to contend, which leads to convoluted lock/unlock sequences that shouldn't exist. Beyond being messy, a shared mutex is a deadlock trap: a public method holds the lock and calls a helper that also locks the same mutex. It works today only because of careful manual unlock placement — the moment someone refactors to `defer mu.Unlock()`, it deadlocks. Separate concerns eliminate the need for shared locks entirely.

#### Stop semantics

`Stop()` is synchronous. When it returns, the operation is fully stopped: goroutines have exited, resources are released, and observable state is final.

Apply the same rule at every layer:

- `WorkPipeline.Stop()` signals the run and waits for it to exit.
- `Scheduler.Close()` waits for in-flight work to drain.
- Service-level `Stop()` detaches references under lock, performs shutdown outside the lock, and returns only after cleanup is complete.

If the underlying work is long-running, `Stop()` may block. That is expected.

#### Async service checklist

1. Prefer pull-based state through methods such as `GetStatus()` or `State()`, not callbacks. It keeps services easier to write and reason about.
2. Use separate mutexes for separate concerns. Do not nest locks.
3. Detach shared references under lock and shut resources down outside the lock.
4. Make `Stop()` mean fully stopped, not "shutdown requested".
5. Use non-blocking channel sends when a run loop may already have exited on its own.
6. Let the database answer terminal state when the database is the true source of truth.
7. Route async work through `work.Pipeline` and `Scheduler`, not raw goroutines.
8. Inject work-unit builders or equivalent seams for testability.
9. Apply backoff before retrying the next attempt.

## Store

The store layer manages all data persistence through DuckDB.
Stores are **always** returning domain models.
Stores do not have buisness logic.

### Structure

Each resource gets its own store struct (`VMStore`, `GroupStore`, `CredentialsStore`, etc.) that holds a `QueryInterceptor`. The `Store` facade creates all resource stores in its constructor and exposes them via accessor methods:

```go
s.VM()           // *VMStore
s.Group()        // *GroupStore
s.Credentials()  // *CredentialsStore
```

### Query building

Use [squirrel](https://github.com/Masterminds/squirrel) for all query construction. Define table and column names as package-level constants to avoid typos. For frequently used queries, store pre-built select builders as package-level variables.

> Sometimes, for very big queries, raw string is acceptable.

### QueryInterceptor

All queries go through the `QueryInterceptor`, which:

- Routes queries to the active transaction or the raw database connection based on context.
- Logs all queries at debug level.
- Runs `FORCE CHECKPOINT` after writes outside transactions (DuckDB WAL mode).

Store methods never touch `*sql.DB` or `*sql.Tx` directly — the interceptor handles this transparently.

### Transactions

Use `store.WithTx(ctx, fn)` for operations that must be atomic. The transaction is attached to context and the `QueryInterceptor` picks it up automatically:

```go
return s.store.WithTx(ctx, func(txCtx context.Context) error {
    // all store calls using txCtx participate in the transaction
    ids, err := s.store.Credentials().List(txCtx)
    // ...
    return s.store.Credentials().SavePassword(txCtx, newHash)
})
```

**DuckDB does not support nested transactions.** `WithTx` will return an error if called within an existing transaction.

Transactions must be called **only in the service layer**. Store methods are simple data access — they do not coordinate across stores or manage transactions. If you find yourself needing to call a method from one store inside another store, something is wrong. That coordination belongs in the service.

### Error handling

Use typed errors from `pkg/errors` for expected conditions: `NewResourceNotFoundError`, `NewDuplicateResourceError`, etc. Check `sql.ErrNoRows` and translate it to the appropriate domain error.

### Functional options

`VMStore.List()` accepts `ListOption` functions that modify the query builder:

```go
vms, err := s.VM().List(ctx, filters, store.WithSort(params), store.WithLimit(10))
```

Built-in options include `WithSort`, `WithLimit`, `WithOffset`, `WithVMIDs`, and `WithDefaultSort`.

### Migrations

SQL migration files live in `internal/store/migrations/sql/` and are numbered sequentially (`001_`, `002_`, etc.). They are embedded and executed automatically on startup. The migration runner tracks applied versions in a `schema_migrations` table.

### Filtering

Every list endpoint that accepts user-supplied filters must use the filter DSL from `pkg/filter/`. Do not hand-build SQL WHERE clauses from query parameters. The filter package provides a lexer, parser, and SQL generator that converts expressions like `memory > 8GB and cluster = 'prod'` into `sq.Sqlizer` objects for use with squirrel queries.

#### Grammar

```text
expression  : term ( "or" term )* ;
term        : factor ( "and" factor )* ;
factor      : equality | "(" expression ")" ;
equality    : IDENTIFIER op value
            | IDENTIFIER ( "~" | "!~" ) REGEX_LITERAL
            | IDENTIFIER "in" "[" STRING ( "," STRING )* "]"
            | IDENTIFIER "not" "in" "[" STRING ( "," STRING )* "]" ;
value       : STRING | QUANTITY | BOOLEAN ;
```

Operators: `=`, `!=`, `<`, `<=`, `>`, `>=`, `~` (regex match), `!~` (regex not match), `in`, `not in`, `and`, `or`. AND binds tighter than OR; use parentheses to override.

Value types: single/double-quoted strings, booleans (`true`/`false`), quantities with optional units (`KB`, `MB`, `GB`, `TB` — normalized to MB), regex patterns between `/slashes/`, and comma-separated string lists in brackets for `in`/`not in`.

#### Field maps

A `MapFunc` maps DSL identifiers to SQL column references and validates field types. The package provides built-in maps:

- `ParseWithDefaultMap` — VM filtering across joined tables. Flat names (`name`, `memory`, `cluster`) map to `vinfo`; dotted names map to joined tables (`disk.capacity`, `concern.category`, `cpu.sockets`, `net.type`, `datastore.name`, etc.).
- `ParseWithGroupMap` — group filtering (`name`, `description`, `filter`).
- `ParseWithClusterMap` — cluster filtering (`cluster_id`, `cluster_name`).

When adding a new filterable resource, define a new `MapFunc` and a corresponding `ParseWithXxxMap` entry point. See `pkg/filter/doc.go` for the full field list.

#### Three-layer flow

Every filter follows the same path through the codebase:

1. **Handler** — validates the expression early and returns 400 on syntax error:

   ```go
   if _, err := filter.ParseWithDefaultMap([]byte(*params.ByExpression)); err != nil {
       c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
       return
   }
   ```

2. **Service** — passes the expression string to the store (or parses it directly for non-VM resources):

   ```go
   filters = append(filters, store.ByFilter(params.Expression))
   vms, err := s.store.VM().List(ctx, filters, opts...)
   ```

3. **Store** — `ByFilter` parses the expression into a `sq.Sqlizer` and applies it as a WHERE clause on the filter subquery. The subquery joins all filterable tables and returns distinct VM IDs, which the main query uses via `WHERE id IN (subquery)`.

Follow this pattern for every new filterable endpoint. Do not skip the handler validation step — it catches bad expressions before they reach the store.

### Error Handling

Use typed domain errors from `pkg/errors` whenever callers need to branch on the error. Do not replace those errors with `fmt.Errorf`, because that loses the type information handlers depend on.

```go
// Define: factory + checker
func NewCollectionInProgressError() *CollectionInProgressError { ... }
func IsOperationInProgressError(err error) bool { ... }

// Use in handlers
if srvErrors.IsOperationInProgressError(err) {
    c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
    return
}
```

When you add a new domain error to `pkg/errors/errors.go`, add both:

- a constructor or factory function
- a checker function


### Testing

Use Ginkgo v2. Prefer table-driven tests when behavior varies by input or state.

Testing rules:

- Never mock the store or the scheduler. Use the real `internal/store` and `pkg/scheduler` implementations in tests.
- For vCenter-backed behavior, prefer `vcsim` whenever it can cover the scenario.
- Mock vCenter operations only when the behavior cannot reasonably be exercised through `vcsim`.
- For async services, inject work-unit builders or similar seams that keep tests focused on service behavior without mocking the scheduler.
- End-to-end tests are expected whenever they are feasible within the vCenter constraints of the workflow.

## Submitting Changes

1. Branch from `main`.
2. Keep each commit focused on one logical change.
3. Add or update focused Ginkgo tests when behavior changes.
4. Run `make format`, `make test`, and `make lint` before opening a PR.
5. Open a pull request and link the related issue.
