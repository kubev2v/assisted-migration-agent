package errors

import (
	"errors"
	"fmt"

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
