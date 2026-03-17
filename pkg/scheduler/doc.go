// Package scheduler implements typed asynchronous work execution with futures,
// priority ordering, and optional reserved worker capacity.
//
// The scheduler exists to solve a specific orchestration problem: services may
// have many independent work items or many pipelines, but still need one shared
// concurrency budget. The scheduler owns that budget. Work is submitted via
// AddWork or AddPriorityWork and returns a Future that exposes the eventual
// result or allows cancellation.
//
// # Rationale
//
// The most important property of the scheduler is not just concurrency, but
// liveness under cancellation pressure.
//
// In the common case, services submit normal work through AddWork. That work
// uses only the normal worker pool. Some workflows also need a small amount of
// capacity that remains available for urgent follow-up work, especially cleanup
// or finalization work triggered when pipelines are stopped.
//
// Reserved workers exist for that reason.
//
// The intent is:
//   - normal work (priority == 0) runs only on normal workers
//   - priority work (priority > 0) may run on normal workers or reserved workers
//
// This is not meant to be a general "importance" ranking system for all work.
// The main use case is to guarantee that cleanup-related work can still make
// progress even when all normal workers are saturated with regular work.
//
// Without reserved workers, a service can request cancellation but have its
// cleanup tasks starved behind the very work it is trying to stop. Reserved
// workers give the scheduler a narrow but important guarantee: priority work
// retains a path to execution when normal capacity is exhausted.
//
// # Ordering Semantics
//
// The work queue is ordered by priority, highest first.
//
// This means the scheduler is no longer FIFO in the general case. Priority is
// the primary ordering rule. Work items with the same priority have no explicit
// stable tie-breaker and should be treated as unspecified order.
//
// # Core Components
//
// Scheduler:
//   - Manages a pool of normal workers and optional reserved workers
//   - Maintains a priority-ordered work queue
//   - Runs an event loop dispatching work to eligible workers
//   - Supports graceful shutdown via Close()
//
// Worker:
//   - Executes a single work function
//   - Returns to the worker pool after completion
//   - Carries whether it belongs to the reserved pool
//   - Recovers from panics and reports them as errors
//
// Future:
//   - Represents a pending result from submitted work
//   - Provides a channel to receive the result
//   - Supports cancellation via Stop()
//
// # Dispatch Rules
//
// Dispatch applies two dimensions at once:
//   - work priority
//   - worker eligibility
//
// Work selection:
//   - Higher-priority work is considered before lower-priority work
//
// Worker eligibility:
//   - Normal work (`priority == 0`) may use only normal workers
//   - Priority work (`priority > 0`) may use normal or reserved workers
//
// If the next queued work item has no eligible worker right now, it remains
// queued and dispatch stops. This is intentional: "no eligible worker" is a
// valid stopping condition, distinct from "no worker exists at all".
//
// Example:
//   - all normal workers busy
//   - reserved workers idle
//   - next queued work has `priority == 0`
//
// In that case the work is not schedulable yet, so the scheduler preserves both
// the queued work and the reserved workers and waits for a future event, such
// as a normal worker completing or a higher-priority work item arriving.
//
// # Constructor Validation
//
// NewScheduler requires at least one normal worker:
//
//	sched, err := scheduler.NewScheduler[string](4, 1)
//
// A scheduler with only reserved workers is rejected. Reserved workers are not
// an independent execution pool; they are supplemental capacity for priority
// work on top of a normal worker pool.
//
// # Work Execution Flow
//
//  1. Client calls AddWork(fn) or AddPriorityWork(fn, priority)
//  2. Scheduler wraps the work with:
//     - a buffered result channel
//     - a cancellable context derived from the scheduler context
//     - a priority value
//  3. The request is sent to the scheduler event loop
//  4. run() enqueues it and calls dispatch()
//  5. dispatch() selects the highest-priority queued work and finds an eligible worker
//  6. The worker executes the work and returns a typed Result on the Future channel
//
// # Cancellation
//
// Each work item gets a context derived from the scheduler's main context:
//
//	ctx, cancel := context.WithCancel(s.mainCtx)
//
// Cancellation hierarchy:
//   - future.Stop() cancels a single work item's context
//   - scheduler.Close() cancels the scheduler's main context, affecting all work
//
// Work functions are expected to observe ctx.Done():
//
//	func(ctx context.Context) (any, error) {
//	    select {
//	    case <-ctx.Done():
//	        return nil, ctx.Err()
//	    default:
//	        return result, nil
//	    }
//	}
//
// # Panic Recovery
//
// Workers recover from panics and convert them into Result errors. This keeps
// the scheduler alive and ensures the Future still resolves.
//
// # Graceful Shutdown
//
// Close() is idempotent and performs graceful shutdown:
//  1. cancel the scheduler context
//  2. ask the event loop to stop
//  3. wait for all in-flight workers to finish
//
// The scheduler does not abandon running work abruptly. Shutdown waits for the
// worker count tracked by the WaitGroup to drain.
//
// # Usage Example
//
//	sched, err := scheduler.NewScheduler[string](4, 1)
//	if err != nil {
//	    return err
//	}
//	defer sched.Close()
//
//	normal := sched.AddWork(func(ctx context.Context) (string, error) {
//	    return "normal", nil
//	})
//
//	priority := sched.AddPriorityWork(func(ctx context.Context) (string, error) {
//	    return "cleanup", nil
//	}, 1)
//
//	_ = <-normal.C()
//	_ = <-priority.C()
package scheduler
