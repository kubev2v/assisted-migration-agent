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
