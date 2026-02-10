package errors

import (
	"errors"
	"fmt"
	"strings"
)

// ResourceNotFoundError indicates a resource was not found.
type ResourceNotFoundError struct {
	Kind string
	ID   string
}

func NewResourceNotFoundError(kind string, id string) *ResourceNotFoundError {
	return &ResourceNotFoundError{Kind: kind, ID: id}
}

func NewInventoryNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("inventory", "")
}

func NewConfigurationNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("configuration", "")
}

func (e *ResourceNotFoundError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("%s '%s' not found", e.Kind, e.ID)
	}
	return fmt.Sprintf("%s not found", e.Kind)
}

func IsResourceNotFoundError(err error) bool {
	var e *ResourceNotFoundError
	return errors.As(err, &e)
}

// CollectionInProgressError indicates a collection is already running.
type CollectionInProgressError struct{}

func NewCollectionInProgressError() *CollectionInProgressError {
	return &CollectionInProgressError{}
}

func (e *CollectionInProgressError) Error() string {
	return "collection already in progress"
}

func IsCollectionInProgressError(err error) bool {
	var e *CollectionInProgressError
	return errors.As(err, &e)
}

// InvalidStateError indicates an invalid state for the requested operation.
type InvalidStateError struct{}

func NewInvalidStateError() *InvalidStateError {
	return &InvalidStateError{}
}

func (e *InvalidStateError) Error() string {
	return "invalid state for this operation"
}

func IsInvalidStateError(err error) bool {
	var e *InvalidStateError
	return errors.As(err, &e)
}

// ModeConflictError indicates a valid request that conflicts with prior fatal state.
type ModeConflictError struct {
	Reason string
}

func NewModeConflictError(reason string) *ModeConflictError {
	return &ModeConflictError{Reason: reason}
}

func (e *ModeConflictError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("mode change conflict: %s", e.Reason)
	}
	return "mode change conflict"
}

func IsModeConflictError(err error) bool {
	var e *ModeConflictError
	return errors.As(err, &e)
}

func NewVCenterError(err error) *VCenterError {
	vErr := &VCenterError{msg: "unknown error"}
	if strings.Contains(err.Error(), "Login failure") ||
		(strings.Contains(err.Error(), "incorrect") && strings.Contains(err.Error(), "password")) {
		vErr.msg = "invalid credentials"
	} else {
		vErr.msg = err.Error()
	}
	return vErr
}

// VCenterError indicates the provided credentials are invalid.
type VCenterError struct {
	msg string
}

func (e *VCenterError) Error() string {
	return e.msg
}

func IsVCenterError(err error) bool {
	var e *VCenterError
	return errors.As(err, &e)
}

// ConsoleClientError wraps HTTP 4xx errors from the console client.
type ConsoleClientError struct {
	StatusCode int
	Message    string
}

func NewConsoleClientError(statusCode int, message string) *ConsoleClientError {
	return &ConsoleClientError{StatusCode: statusCode, Message: message}
}

func (e *ConsoleClientError) Error() string {
	return fmt.Sprintf("console client error %d: %s", e.StatusCode, e.Message)
}

func IsConsoleClientError(err error) bool {
	var e *ConsoleClientError
	return errors.As(err, &e)
}

// InspectorWorkError indicates that an error occurred during the work
type InspectorWorkError struct {
	msg string
}

func NewInspectorWorkError(format string, args ...any) error {
	return &InspectorWorkError{
		msg: fmt.Sprintf(format, args...),
	}
}

func (e *InspectorWorkError) Error() string {
	return e.msg
}

// InspectorNotRunningError indicates that inspector not currently running
type InspectorNotRunningError struct{}

func NewInspectorNotRunningError() *InspectorNotRunningError {
	return &InspectorNotRunningError{}
}

func (e *InspectorNotRunningError) Error() string {
	return "inspector not running"
}
