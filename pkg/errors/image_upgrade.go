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
