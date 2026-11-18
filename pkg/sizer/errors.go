package sizer

import "fmt"

// NotEnoughResourcesError describes a request that no eligible node can fit.
type NotEnoughResourcesError struct {
	RequestedCPU    float64
	RequestedMemory float64
}

// NewNotEnoughResourcesError creates a resource error with the requested totals.
func NewNotEnoughResourcesError(requestedCPU, requestedMemory float64) *NotEnoughResourcesError {
	return &NotEnoughResourcesError{
		RequestedCPU:    requestedCPU,
		RequestedMemory: requestedMemory,
	}
}

// Error returns the resource failure message.
func (e *NotEnoughResourcesError) Error() string {
	return fmt.Sprintf(
		"not enough resources: requested %g CPU and %g GB memory",
		e.RequestedCPU,
		e.RequestedMemory,
	)
}
