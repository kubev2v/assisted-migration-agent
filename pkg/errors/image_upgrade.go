package errors

import "errors"

func NewImageUpgradeInProgressError() *OperationInProgressError {
	return NewOperationInProgressError("image upgrade")
}

func IsImageUpgradeInProgressError(err error) bool {
	var e *OperationInProgressError
	return errors.As(err, &e) && e.operation == "image upgrade"
}

type InvalidImageUpgradeImageError struct{ Message string }

func NewInvalidImageUpgradeImageError(message string) *InvalidImageUpgradeImageError {
	return &InvalidImageUpgradeImageError{Message: message}
}

func (e *InvalidImageUpgradeImageError) Error() string { return e.Message }

func IsInvalidImageUpgradeImageError(err error) bool {
	var e *InvalidImageUpgradeImageError
	return errors.As(err, &e)
}

type ImageUpgradePayloadTooLargeError struct{}

func NewImageUpgradePayloadTooLargeError() *ImageUpgradePayloadTooLargeError {
	return &ImageUpgradePayloadTooLargeError{}
}

func (e *ImageUpgradePayloadTooLargeError) Error() string { return "upgrade image exceeds 3 GiB" }

func IsImageUpgradePayloadTooLargeError(err error) bool {
	var e *ImageUpgradePayloadTooLargeError
	return errors.As(err, &e)
}

type ImageUpgradeInsufficientStorageError struct{}

func NewImageUpgradeInsufficientStorageError() *ImageUpgradeInsufficientStorageError {
	return &ImageUpgradeInsufficientStorageError{}
}

func (e *ImageUpgradeInsufficientStorageError) Error() string {
	return "insufficient storage for upgrade image"
}

func IsImageUpgradeInsufficientStorageError(err error) bool {
	var e *ImageUpgradeInsufficientStorageError
	return errors.As(err, &e)
}

// NoImageUpgradeError means the console offers no image the appliance can apply in place.
type NoImageUpgradeError struct{ Reason string }

func NewNoImageUpgradeError(reason string) *NoImageUpgradeError {
	return &NoImageUpgradeError{Reason: reason}
}

func (e *NoImageUpgradeError) Error() string { return e.Reason }

func IsNoImageUpgradeError(err error) bool {
	var e *NoImageUpgradeError
	return errors.As(err, &e)
}

// ImageUpgradeConsoleError means the console could not report or serve the upgrade image.
type ImageUpgradeConsoleError struct{ Err error }

func NewImageUpgradeConsoleError(err error) *ImageUpgradeConsoleError {
	return &ImageUpgradeConsoleError{Err: err}
}

func (e *ImageUpgradeConsoleError) Error() string { return "console: " + e.Err.Error() }

func (e *ImageUpgradeConsoleError) Unwrap() error { return e.Err }

func IsImageUpgradeConsoleError(err error) bool {
	var e *ImageUpgradeConsoleError
	return errors.As(err, &e)
}
