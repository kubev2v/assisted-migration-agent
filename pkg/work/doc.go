// Package work provides execution abstractions for sequencing async work
// through a typed scheduler.
//
// # Core Concepts
//
// There are two families of abstractions:
//
// Pull-based (Pipeline, Service, Pool) — the caller starts pipelines and
// polls for state. Cleanup is the caller's responsibility.
//
// Push-based (Pipeline2, Pool2) — the pool actively drives pipeline
// progression, pushes status events to the caller, and owns the cleanup
// process end-to-end.
//
// Pull-based types:
//
//   - Pipeline — a reusable, sequential executor that can be restarted after
//     each run completes. It is the low-level building block.
//
//   - Service — a one-time consumable wrapper around a single Pipeline with an
//     internal scheduler. Once Start() returns or Stop() is called, the Service
//     cannot be restarted.
//
//   - Pool — a one-time consumable set of keyed Pipelines sharing one scheduler.
//     Like Service, it enforces single-start semantics.
//
// # Pipeline
//
// Pipeline executes a sequence of WorkUnit[S, R] steps through an externally
// provided Scheduler. Each unit receives the result of the previous unit,
// forming a chain. S is the status type reported before each step; R is the
// result type threaded through.
//
// Pipeline does not own a scheduler. The caller creates the scheduler, passes
// it in, and is responsible for closing it. This allows multiple pipelines to
// share one scheduler and one concurrency budget.
//
// Pipeline is reusable: once a run completes (naturally or via Stop), a new
// run can be started on the same instance. This makes it suitable for run
// loops that repeatedly create and start pipelines against a shared scheduler
// (e.g. Console's dispatch loop).
//
// Cancellation: Stop() closes an internal channel to signal the goroutine.
// The pipeline reports ErrStopped (not context.Canceled) because cancellation
// is driven by the pipeline itself, not an external context.
//
// Error semantics: the first unit that returns an error stops execution.
// Remaining units never run. The error is readable via State().Err.
//
// Use Pipeline directly only when you need to manage the scheduler yourself
// (e.g. Console's run loop creates a single scheduler and builds a new
// pipeline on every iteration). For most services, use Service or Pool.
//
// # Service
//
// Service wraps a single Pipeline with its own Scheduler (1 worker, 0 reserved).
// It exists for the common case: one builder, one sequential pipeline, no
// shared concurrency budget.
//
// Service is one-time consumable: create → start → read state → discard.
// The constructor takes the initial state and a WorkBuilder. Start() creates
// the scheduler and pipeline internally. Calling Start() twice returns
// ServiceAlreadyStartedError. Stop() cancels the pipeline and closes the
// scheduler; state remains readable afterward.
//
// A coordinator service (e.g. CollectorService) manages disposable Service
// instances: it checks preconditions, creates a new Service for each run,
// and reads state from the current instance.
//
// # Pool
//
// Pool wraps multiple Pipelines keyed by string, sharing one Scheduler with
// a configurable number of workers. It exists for the case where several
// independent work streams run concurrently against a shared worker budget
// (e.g. one pipeline per VM during inspection).
//
// Pool is one-time consumable, like Service. The constructor takes the worker
// count and a map of PoolEntry (initial state + builder per key). Start()
// creates the scheduler and starts all pipelines. Cancel(key) stops a single
// pipeline. State(key) returns the per-key status or an error if the key does
// not exist. IsRunning() returns true if any pipeline is still active.
//
// # Pipeline2
//
// Pipeline2 is functionally equivalent to Pipeline but designed for the
// push-based model. It streams Status events on the channel returned by
// Start() rather than requiring the caller to poll State(). The caller
// reads from the channel to observe progress. When the channel closes,
// the pipeline has completed.
//
// # Pool2
//
// Pool2 wraps multiple Pipeline2 instances sharing one Scheduler. Unlike
// Pool, it takes an active role: a central run loop reads status events
// from all pipelines and maintains per-pipeline state. Finalization is
// built into the contract via WorkBuilder2:
//
//   - Per-pipeline finalize: each builder implements Finalize(ctx, result)
//     which runs as priority work after that pipeline completes. The final
//     result is passed in so cleanup can act on what was produced. Errors
//     are surfaced via State(key).Err.
//
//   - Pool-level finalize: an optional function set via WithFinalizer runs
//     as priority work after all pipelines have finished. Its error is
//     returned by Stop().
//
//   - Stop() blocks until all pipelines and all finalization have fully
//     terminated, then returns the pool-level finalize error (if any).
//
// This stronger contract means the caller never has to coordinate cleanup
// ordering or wonder whether background work is still running.
//
// # Lifecycle Summary
//
//	Pipeline:  NewPipeline(state, sched, builder) → Start() → State() / Stop()  (restartable after completion)
//	Service:   NewService(state, builder)         → Start() → State() / Stop()  (single-start, then discard)
//	Pool:      NewPool(workers, entries)          → Start() → State(key) / Cancel(key) / Stop()  (single-start, then discard)
//	Pipeline2: NewPipeline2(sched, builder)       → Start() → <-chan / Stop()   (single-start)
//	Pool2:     NewPool2(builders).WithFinalizer(fn) → Start() → State(key) / Cancel(key) / Stop()  (single-start, Stop blocks)
//
// After Start():
//   - State() is always valid and returns the current or final status.
//   - IsRunning() reports whether the goroutine(s) are still active.
//   - Stop() is idempotent and safe to call concurrently.
//   - After completion or Stop(), result and error persist on the instance.
//   - For Service and Pool, the instance is never reused. Create a new one for the next run.
//   - For Pipeline, a new run can be started after the previous one completes.
//   - For Pool2, Stop() blocks until all pipelines and finalization complete, returning the pool-level finalize error.
package work
