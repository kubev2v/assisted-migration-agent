# Scheduler and Pipelines

This document describes how services should use `Scheduler[R]` and `WorkPipeline[S, R]`, and why the ownership boundaries are set up the way they are.

## Goals

The service orchestration model is designed to preserve three things at once:

- clear ownership of execution state
- shared concurrency control
- compile-time type safety

`WorkPipeline[S, R]` solves per-run sequencing and state tracking. `Scheduler[R]` solves concurrency control across many work items or many pipelines. The scheduler type parameter is the same `R` that flows through the pipeline. Services compose the two.

## Responsibilities

### `WorkPipeline[S, R]`

`WorkPipeline` owns only per-run execution state:

- the current state `S`
- the accumulated result `R`
- the terminal error
- stop and completion coordination for that single run

It does not own service lifecycle, cleanup callbacks, or shared concurrency policy.

### `Scheduler[R]`

`Scheduler[R]` owns concurrency:

- worker pool size
- dispatching work items
- throttling a shared execution budget

It is useful only when multiple work items or multiple pipelines share it.

The scheduler may also reserve a small amount of worker capacity for
priority work. This exists to preserve liveness during cancellation and stop
flows, not to create a general-purpose "importance" ranking for all work.

### Service

The service owns orchestration:

- creating pipelines
- deciding which scheduler instance to use
- deciding whether one or many pipelines are allowed
- interpreting pipeline state into service-level status
- deciding what the real source of truth is for terminal state

## Why the Scheduler Belongs in the Service

The scheduler should stay at the service layer.

If the scheduler moves above the service layer, type safety gets weaker. The service is the natural boundary where the concrete result type is known. That is where a typed `Scheduler[R]` still makes sense without pushing generic plumbing through unrelated parts of the system.

If the scheduler moves below the service layer into each pipeline, the scheduler loses its purpose. A per-pipeline scheduler cannot enforce a shared concurrency limit across many pipelines. That breaks important workflows such as inventory collection, where there may be hundreds of pipelines but only one shared concurrency budget, for example `5`.

So the intended layering is:

- `WorkPipeline[S, R]`: per-run typed sequencing and state
- service: typed orchestration and shared scheduler ownership
- `Scheduler[R]`: shared concurrency control for one workflow domain

## Why Reserved Workers Exist

Reserved workers solve a specific problem: cleanup or finalization work must
still make progress even when normal capacity is saturated.

This matters for stop semantics. A pipeline may be canceled while all normal
workers are busy running regular work. If cleanup units are queued behind that
same normal capacity, the stop request can stall because the cleanup needed to
finish the stop has no path to execution.

Reserved workers prevent that starvation.

The intended semantics are:

- normal work (`priority == 0`) uses only normal workers
- priority work (`priority > 0`) may use normal workers or reserved workers

The important point is the rationale: reserved workers are primarily for
cleanup-related work triggered during stop or cancellation flows. They are not
meant to be a general scheduling policy for "more important" business work.

With that model:

- normal throughput is still governed by the normal worker pool
- stop/cleanup work retains a path to execution
- services can preserve existing stop semantics while still enqueueing cleanup units

Without reserved workers, a service can request stop but have its cleanup work
starved behind the very work it is trying to cancel.

## Why `WorkPipeline` Has No Callbacks

`WorkPipeline` used to push completion or status information back into services. That made the generic layer carry caller-specific semantics and introduced avoidable races around stale completions, cleanup, and ownership.

The current model is pull-based:

- pipeline updates its own state
- service reads that state with `State()` and `IsRunning()`
- service decides how to interpret it

This keeps the pipeline generic and self-contained. It also lets services tolerate stale pipelines instead of trying to eagerly reconcile completion with service state.

## Why `initialState` Is Required

There is a real gap between:

1. `Start()` returning
2. the first unit actually starting on the scheduler

Without an explicit initial state, the service would need to invent some pending or queued status outside the pipeline. That would split state ownership across two places.

Requiring `initialState` keeps state ownership inside the pipeline while allowing each service to choose semantics that fit its workflow.

Examples:

- collector may start with `connecting`
- another service may prefer `pending`
- a bulk inventory service may choose `queued`

## Recommended Service Pattern

Services should generally follow this pattern:

1. Own a typed shared scheduler when the workflow needs concurrency control.
2. Create a pipeline with an explicit initial state.
3. Start the pipeline.
4. Read pipeline state from `GetStatus()` instead of receiving callbacks.
5. Use the service's own source of truth for terminal state when appropriate.

## Collector Pattern

`CollectorService` is a useful example of a service that tolerates stale pipelines.

Its `GetStatus()` logic is intentionally ordered like this:

1. Look in the database for inventory.
2. If inventory exists, return `collected`.
3. Otherwise, if a pipeline exists, return the pipeline state or error.
4. Otherwise return `ready`.

This works because the database is the authoritative source of "collected", while the pipeline is only the source of in-flight state.

The service does not need to nil the pipeline on natural completion. A completed pipeline can remain attached and be safely ignored once the database answers the terminal question.

## Many-Pipeline Pattern

Some services need many concurrent pipelines that all share one execution budget.

Example:

- inventory service may run one pipeline per VM
- each pipeline has the same result type
- all pipelines share one `Scheduler[R]`
- the scheduler enforces a limit such as `5`

That is the main reason scheduler ownership must remain at the service layer rather than inside `WorkPipeline`.

## Summary

Use the following rule of thumb:

- if it is about one run's state, it belongs in `WorkPipeline`
- if it is about concurrency across runs, it belongs in `Scheduler`
- if it is about orchestration, lifecycle, or interpretation, it belongs in the service
