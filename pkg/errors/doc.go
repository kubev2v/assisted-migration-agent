// Package errors provides custom error types for the assisted-migration-agent.
//
// Each error type includes a constructor, Error() method, and a type-checking
// helper using errors.As for proper error unwrapping.
//
// # Error Types Overview
//
//	┌──────────────────────────┬────────┬─────────────────────────────────────┐
//	│ Error Type               │ HTTP   │ Description                         │
//	├──────────────────────────┼────────┼─────────────────────────────────────┤
//	│ ResourceNotFoundError    │ 404    │ Requested resource doesn't exist    │
//	│ CollectionInProgressError│ 409    │ Collection already running          │
//	│ InvalidStateError        │ 500    │ Invalid state for operation         │
//	│ ModeConflictError        │ 409    │ Mode change blocked by fatal error  │
//	│ VCenterError             │ 500    │ vCenter connection/auth failure     │
//	│ ConsoleClientError       │ 4xx    │ HTTP error from console.redhat.com  │
//	└──────────────────────────┴────────┴─────────────────────────────────────┘
//
// # ResourceNotFoundError
//
// Indicates a requested resource was not found in the store.
//
// Constructors:
//   - NewResourceNotFoundError(kind string) - Generic resource not found
//   - NewInventoryNotFoundError() - Inventory not collected yet
//   - NewConfigurationNotFoundError() - Configuration not found
//
// Usage:
//
//	if errors.IsResourceNotFoundError(err) {
//	    c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
//	}
//
// # CollectionInProgressError
//
// Indicates an attempt to start collection while one is already running.
// The collector only allows one collection at a time.
//
// Constructor:
//   - NewCollectionInProgressError()
//
// Usage:
//
//	if errors.IsCollectionInProgressError(err) {
//	    c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
//	}
//
// # InvalidStateError
//
// Indicates the operation cannot be performed in the current state.
//
// Constructor:
//   - NewInvalidStateError()
//
// Usage:
//
//	if errors.IsInvalidStateError(err) {
//	    c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
//	}
//
// # ModeConflictError
//
// Indicates a mode change was rejected due to a prior fatal error.
// This occurs when the console service received a 4xx error and stopped,
// preventing further mode changes to avoid retry loops.
//
// Constructor:
//   - NewModeConflictError(reason string)
//
// Usage:
//
//	if errors.IsModeConflictError(err) {
//	    c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
//	}
//
// # VCenterError
//
// Wraps errors from vCenter connections with user-friendly messages.
// Automatically detects login failures and credential issues.
//
// Constructor:
//   - NewVCenterError(err error) - Wraps and interprets the underlying error
//
// Error detection:
//   - "Login failure" or "incorrect password" → "invalid credentials"
//   - Other errors → Original error message
//
// Usage:
//
//	if errors.IsVCenterError(err) {
//	    // Handle vCenter-specific error
//	}
//
// # ConsoleClientError
//
// Wraps HTTP 4xx errors from the console.redhat.com API.
// These are fatal errors that cause the console service to stop.
//
// Constructor:
//   - NewConsoleClientError(statusCode int, message string)
//
// Fields:
//   - StatusCode: HTTP status code (e.g., 401, 410)
//   - Message: Error message from server
//
// Usage:
//
//	if errors.IsConsoleClientError(err) {
//	    // Fatal error - console service should stop
//	}
//
// # Type Checking Pattern
//
// All error types provide Is* helper functions that use errors.As
// for proper error chain unwrapping:
//
//	func IsResourceNotFoundError(err error) bool {
//	    var e *ResourceNotFoundError
//	    return errors.As(err, &e)
//	}
//
// This allows checking wrapped errors:
//
//	wrapped := fmt.Errorf("operation failed: %w", errors.NewInventoryNotFoundError())
//	errors.IsResourceNotFoundError(wrapped) // returns true
//
// # Handler Error Mapping
//
// Handlers typically map errors to HTTP status codes:
//
//	switch {
//	case errors.IsResourceNotFoundError(err):
//	    c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
//	case errors.IsCollectionInProgressError(err):
//	    c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
//	case errors.IsModeConflictError(err):
//	    c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
//	default:
//	    c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
//	}
package errors
