// Package work provides execution abstractions for sequencing async work
// through a typed scheduler.
//
// # Pipeline
//
// Pipeline executes a sequence of WorkUnit steps through an external
// Scheduler. Each unit receives the result of the previous unit,
// forming a chain. S is the status type reported before each step; R is the
// result type threaded through.
//
// Pipeline does not own a scheduler. The caller creates the scheduler, passes
// it in, and is responsible for closing it. This allows multiple pipelines to
// share one scheduler and one concurrency budget.
//
// Start() returns a chan struct{} that emits one tick before each work
// unit runs and one tick on error. When the channel closes, the pipeline
// has completed (naturally, on error, or via Stop).
//
// Pipeline owns its state internally via a progress struct with its own
// mutex, separating data protection from control flow. State() returns
// the status set before the current unit ran. Result() returns the
// accumulated result and any error.
//
// Pipeline requires a WorkBuilder, which includes Finalize(ctx, result).
// Finalize runs as priority work on the scheduler after the work loop
// exits — including on Stop or error — ensuring cleanup always happens
// regardless of how the pipeline ends.
//
// # Pool
//
// Pool wraps multiple Pipeline instances sharing one Scheduler. Each
// pipeline runs independently; a per-pipeline goroutine drains ticks and
// sends a single done event when the pipeline completes. A central run
// loop processes done events and tracks completion.
//
// Finalization is built into the contract at two levels:
//
//   - Per-pipeline finalize: each WorkBuilder implements Finalize(ctx, result)
//     which runs as priority work inside Pipeline after that pipeline's work
//     loop exits. Errors are surfaced via Result(key).
//
//   - Pool-level finalize: an optional function set via WithFinalizer runs
//     as priority work after all pipelines have finished. Its error is
//     returned by Stop().
//
//   - Stop() blocks until all pipelines and all finalization have fully
//     terminated, then returns the pool-level finalize error (if any).
//
// # Lifecycle Summary
//
//	Pipeline: NewPipeline(sched, builder)             → Start() → <-ticks / State() / Result() / Stop()  (single-start)
//	Pool:     NewPool(builders).WithFinalizer(fn)      → Start() → State(key) / Result(key) / Cancel(key) / Stop()  (single-start, Stop blocks)
//
// After Start():
//   - State() is always valid and returns the current or final status.
//   - IsRunning() reports whether the goroutine(s) are still active.
//   - Stop() is idempotent and safe to call concurrently.
//   - After completion or Stop(), result and error persist on the instance.
package work
