package errors

import (
	"errors"
	"fmt"
	"strings"
)

// ServiceAlreadyStartedError indicates that a work service or pool has already been started.
type ServiceAlreadyStartedError struct{}

func NewServiceAlreadyStartedError() *ServiceAlreadyStartedError {
	return &ServiceAlreadyStartedError{}
}

func (e *ServiceAlreadyStartedError) Error() string {
	return "service already started"
}

func IsServiceAlreadyStartedError(err error) bool {
	var e *ServiceAlreadyStartedError
	return errors.As(err, &e)
}

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

func NewVddkNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("vddk", "")
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

// DuplicateResourceError indicates a resource already exists with the same unique identifier.
type DuplicateResourceError struct {
	Kind  string
	Field string
	Value string
}

func NewDuplicateResourceError(kind, field, value string) *DuplicateResourceError {
	return &DuplicateResourceError{Kind: kind, Field: field, Value: value}
}

func (e *DuplicateResourceError) Error() string {
	return fmt.Sprintf("%s with %s '%s' already exists", e.Kind, e.Field, e.Value)
}

func IsDuplicateResourceError(err error) bool {
	var e *DuplicateResourceError
	return errors.As(err, &e)
}

// OperationInProgressError indicates that the operation is already running.
type OperationInProgressError struct {
	operation string
}

func NewOperationInProgressError(op string) *OperationInProgressError {
	return &OperationInProgressError{
		operation: op,
	}
}

func NewInspectionInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("inspection")
}

func NewCollectionInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("collection")
}

func NewRightsizingCollectionInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("rightsizing collection")
}

func NewVddkUploadInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("vddk upload")
}

func (e *OperationInProgressError) Error() string {
	return fmt.Sprintf("%s already in progress", e.operation)
}

func IsOperationInProgressError(err error) bool {
	var e *OperationInProgressError
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

// InspectorNotRunningError indicates that inspector not currently running
type InspectorNotRunningError struct{}

func NewInspectorNotRunningError() *InspectorNotRunningError {
	return &InspectorNotRunningError{}
}

func (e *InspectorNotRunningError) Error() string {
	return "inspector not running"
}

func IsInspectorNotRunningError(err error) bool {
	var e *InspectorNotRunningError
	return errors.As(err, &e)
}

// InspectionLimitReachedError indicates the configured per-cycle VM inspection limit was exceeded.
type InspectionLimitReachedError struct {
	Limit int
}

func NewInspectionLimitReachedError(limit int) *InspectionLimitReachedError {
	return &InspectionLimitReachedError{Limit: limit}
}

func (e *InspectionLimitReachedError) Error() string {
	return fmt.Sprintf("inspection limit reached (%d VMs per cycle)", e.Limit)
}

func IsInspectionLimitReachedError(err error) bool {
	var e *InspectionLimitReachedError
	return errors.As(err, &e)
}

// CredentialsNotSetError indicates that required credentials were not set
type CredentialsNotSetError struct{}

func NewCredentialsNotSetError() *CredentialsNotSetError {
	return &CredentialsNotSetError{}
}

func (e *CredentialsNotSetError) Error() string {
	return "credentials not set"
}

func IsCredentialsNotSetError(err error) bool {
	var e *CredentialsNotSetError
	return errors.As(err, &e)
}

// UnknownEventKindError indicates an event kind that has no registered handler.
type UnknownEventKindError struct {
	Kind string
}

func NewUnknownEventKindError(kind string) *UnknownEventKindError {
	return &UnknownEventKindError{Kind: kind}
}

func (e *UnknownEventKindError) Error() string {
	return fmt.Sprintf("unknown event kind: %s", e.Kind)
}

func IsUnknownEventKindError(err error) bool {
	var e *UnknownEventKindError
	return errors.As(err, &e)
}
