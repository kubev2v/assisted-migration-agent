package errors

import "errors"

func NewUpdateInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("update")
}

func IsUpdateInProgressError(err error) bool {
	var e *OperationInProgressError
	return errors.As(err, &e) && e.operation == "update"
}

type InvalidUpdateImageError struct{ Message string }

func NewInvalidUpdateImageError(message string) *InvalidUpdateImageError {
	return &InvalidUpdateImageError{Message: message}
}

func (e *InvalidUpdateImageError) Error() string { return e.Message }

func IsInvalidUpdateImageError(err error) bool {
	var e *InvalidUpdateImageError
	return errors.As(err, &e)
}

type UpdatePayloadTooLargeError struct{}

func NewUpdatePayloadTooLargeError() *UpdatePayloadTooLargeError {
	return &UpdatePayloadTooLargeError{}
}

func (e *UpdatePayloadTooLargeError) Error() string { return "update image exceeds 3 GiB" }

func IsUpdatePayloadTooLargeError(err error) bool {
	var e *UpdatePayloadTooLargeError
	return errors.As(err, &e)
}

type InsufficientStorageError struct{}

func NewInsufficientStorageError() *InsufficientStorageError { return &InsufficientStorageError{} }

func (e *InsufficientStorageError) Error() string { return "insufficient storage for update image" }

func IsInsufficientStorageError(err error) bool {
	var e *InsufficientStorageError
	return errors.As(err, &e)
}
