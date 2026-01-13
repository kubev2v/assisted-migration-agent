package errors

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// SourceGoneError indicates the source has been deleted or is no longer available.
type SourceGoneError struct {
	SourceID uuid.UUID
}

func NewSourceGoneError(sourceID uuid.UUID) *SourceGoneError {
	return &SourceGoneError{SourceID: sourceID}
}

func (e *SourceGoneError) Error() string {
	return fmt.Sprintf("source gone: %s", e.SourceID)
}

// IsSourceGoneError checks if the error is a SourceGoneError.
func IsSourceGoneError(err error) bool {
	var e *SourceGoneError
	return errors.As(err, &e)
}

// AgentUnauthorizedError indicates the agent is not authorized to perform the operation.
type AgentUnauthorizedError struct{}

func NewAgentUnauthorized() *AgentUnauthorizedError {
	return &AgentUnauthorizedError{}
}

func (e *AgentUnauthorizedError) Error() string {
	return "agent not authorized"
}

// IsAgentUnauthorizedError checks if the error is an AgentUnauthorizedError.
func IsAgentUnauthorizedError(err error) bool {
	var e *AgentUnauthorizedError
	return errors.As(err, &e)
}

// ResourceNotFoundError indicates a resource was not found.
type ResourceNotFoundError struct {
	Kind string
}

func NewResourceNotFoundError(kind string) *ResourceNotFoundError {
	return &ResourceNotFoundError{Kind: kind}
}

func NewInventoryNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("inventory")
}

func NewCredentialsNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("credentials")
}

func NewConfigurationNotFoundError() *ResourceNotFoundError {
	return NewResourceNotFoundError("configuration")
}

func (e *ResourceNotFoundError) Error() string {
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

// InspectorInProgressError indicates that an inspector is already running.
type InspectorInProgressError struct{}

func NewInspectionInProgressError() *InspectorInProgressError {
	return &InspectorInProgressError{}
}

func (e *InspectorInProgressError) Error() string {
	return "inspection already in progress"
}

// InspectorWorkError
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

type InspectorNonExistVmError struct {
	msg string
}

func NewInspectorNonExistVmError(format string, args ...any) error {
	return &InspectorNonExistVmError{
		msg: fmt.Sprintf(format, args...),
	}
}

func (e *InspectorNonExistVmError) Error() string {
	return e.msg
}
