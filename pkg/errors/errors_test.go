package errors_test

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Errors", func() {
	Context("ResourceNotFoundError", func() {
		// Given a ResourceNotFoundError with a kind and ID
		// When Error() is called
		// Then it should include both the kind and ID
		It("should format message with kind and ID", func() {
			// Arrange
			err := srvErrors.NewResourceNotFoundError("group", "42")

			// Act
			msg := err.Error()

			// Assert
			Expect(msg).To(Equal("group '42' not found"))
		})

		// Given a ResourceNotFoundError with an empty ID
		// When Error() is called
		// Then it should omit the ID from the message
		It("should format message without ID when empty", func() {
			// Arrange
			err := srvErrors.NewResourceNotFoundError("inventory", "")

			// Act
			msg := err.Error()

			// Assert
			Expect(msg).To(Equal("inventory not found"))
		})

		// Given a ResourceNotFoundError
		// When checked with IsResourceNotFoundError
		// Then it should return true
		It("should be detected by IsResourceNotFoundError", func() {
			// Arrange
			err := srvErrors.NewResourceNotFoundError("vm", "123")

			// Act & Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given a ResourceNotFoundError wrapped with fmt.Errorf
		// When checked with IsResourceNotFoundError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			inner := srvErrors.NewResourceNotFoundError("vm", "123")
			wrapped := fmt.Errorf("operation failed: %w", inner)

			// Act & Assert
			Expect(srvErrors.IsResourceNotFoundError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsResourceNotFoundError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Arrange
			err := errors.New("something else")

			// Act & Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeFalse())
		})

		// Given NewInventoryNotFoundError
		// When checked with IsResourceNotFoundError
		// Then it should return true
		It("should detect inventory shorthand as ResourceNotFoundError", func() {
			// Arrange
			err := srvErrors.NewInventoryNotFoundError()

			// Act & Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(err.Error()).To(Equal("inventory not found"))
		})

		// Given NewConfigurationNotFoundError
		// When checked with IsResourceNotFoundError
		// Then it should return true
		It("should detect configuration shorthand as ResourceNotFoundError", func() {
			// Arrange
			err := srvErrors.NewConfigurationNotFoundError()

			// Act & Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(err.Error()).To(Equal("configuration not found"))
		})
	})

	Context("DuplicateResourceError", func() {
		// Given a DuplicateResourceError
		// When Error() is called
		// Then it should include kind, field, and value
		It("should format message with kind, field, and value", func() {
			// Arrange
			err := srvErrors.NewDuplicateResourceError("group", "name", "production")

			// Act
			msg := err.Error()

			// Assert
			Expect(msg).To(Equal("group with name 'production' already exists"))
		})

		// Given a DuplicateResourceError
		// When checked with IsDuplicateResourceError
		// Then it should return true
		It("should be detected by IsDuplicateResourceError", func() {
			// Arrange
			err := srvErrors.NewDuplicateResourceError("group", "name", "prod")

			// Act & Assert
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})

		// Given a DuplicateResourceError wrapped with fmt.Errorf
		// When checked with IsDuplicateResourceError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			inner := srvErrors.NewDuplicateResourceError("group", "name", "prod")
			wrapped := fmt.Errorf("create failed: %w", inner)

			// Act & Assert
			Expect(srvErrors.IsDuplicateResourceError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsDuplicateResourceError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Arrange
			err := errors.New("something else")

			// Act & Assert
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeFalse())
		})
	})

	Context("CollectionInProgressError", func() {
		// Given a CollectionInProgressError
		// When Error() is called
		// Then it should return the expected message
		It("should format the message", func() {
			// Arrange
			err := srvErrors.NewCollectionInProgressError()

			// Act
			msg := err.Error()

			// Assert
			Expect(msg).To(Equal("collection already in progress"))
		})

		// Given a CollectionInProgressError
		// When checked with IsCollectionInProgressError
		// Then it should return true
		It("should be detected by IsCollectionInProgressError", func() {
			// Arrange
			err := srvErrors.NewCollectionInProgressError()

			// Act & Assert
			Expect(srvErrors.IsOperationInProgressError(err)).To(BeTrue())
		})

		// Given a CollectionInProgressError wrapped with fmt.Errorf
		// When checked with IsCollectionInProgressError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("start failed: %w", srvErrors.NewCollectionInProgressError())

			// Act & Assert
			Expect(srvErrors.IsOperationInProgressError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsCollectionInProgressError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsOperationInProgressError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("InvalidStateError", func() {
		// Given an InvalidStateError
		// When Error() is called
		// Then it should return the expected message
		It("should format the message", func() {
			// Arrange
			err := srvErrors.NewInvalidStateError()

			// Act & Assert
			Expect(err.Error()).To(Equal("invalid state for this operation"))
		})

		// Given an InvalidStateError
		// When checked with IsInvalidStateError
		// Then it should return true
		It("should be detected by IsInvalidStateError", func() {
			// Act & Assert
			Expect(srvErrors.IsInvalidStateError(srvErrors.NewInvalidStateError())).To(BeTrue())
		})

		// Given an InvalidStateError wrapped with fmt.Errorf
		// When checked with IsInvalidStateError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("op failed: %w", srvErrors.NewInvalidStateError())

			// Act & Assert
			Expect(srvErrors.IsInvalidStateError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsInvalidStateError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsInvalidStateError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("ModeConflictError", func() {
		// Given a ModeConflictError with a reason
		// When Error() is called
		// Then it should include the reason
		It("should format message with reason", func() {
			// Arrange
			err := srvErrors.NewModeConflictError("console returned 410")

			// Act & Assert
			Expect(err.Error()).To(Equal("mode change conflict: console returned 410"))
		})

		// Given a ModeConflictError with an empty reason
		// When Error() is called
		// Then it should return the base message without a reason
		It("should format message without reason when empty", func() {
			// Arrange
			err := srvErrors.NewModeConflictError("")

			// Act & Assert
			Expect(err.Error()).To(Equal("mode change conflict"))
		})

		// Given a ModeConflictError
		// When checked with IsModeConflictError
		// Then it should return true
		It("should be detected by IsModeConflictError", func() {
			// Act & Assert
			Expect(srvErrors.IsModeConflictError(srvErrors.NewModeConflictError("reason"))).To(BeTrue())
		})

		// Given a ModeConflictError wrapped with fmt.Errorf
		// When checked with IsModeConflictError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("set mode: %w", srvErrors.NewModeConflictError("fatal"))

			// Act & Assert
			Expect(srvErrors.IsModeConflictError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsModeConflictError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsModeConflictError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("VCenterError", func() {
		// Given an error containing "Login failure"
		// When NewVCenterError wraps it
		// Then the message should be "invalid credentials"
		It("should detect login failure", func() {
			// Arrange
			err := srvErrors.NewVCenterError(errors.New("Login failure for user admin"))

			// Act & Assert
			Expect(err.Error()).To(Equal("invalid credentials"))
		})

		// Given an error containing "incorrect password"
		// When NewVCenterError wraps it
		// Then the message should be "invalid credentials"
		It("should detect incorrect password", func() {
			// Arrange
			err := srvErrors.NewVCenterError(errors.New("incorrect user name or password"))

			// Act & Assert
			Expect(err.Error()).To(Equal("invalid credentials"))
		})

		// Given an error with an unrecognized message
		// When NewVCenterError wraps it
		// Then the original message should be preserved
		It("should preserve original message for other errors", func() {
			// Arrange
			err := srvErrors.NewVCenterError(errors.New("connection refused"))

			// Act & Assert
			Expect(err.Error()).To(Equal("connection refused"))
		})

		// Given a VCenterError
		// When checked with IsVCenterError
		// Then it should return true
		It("should be detected by IsVCenterError", func() {
			// Arrange
			err := srvErrors.NewVCenterError(errors.New("timeout"))

			// Act & Assert
			Expect(srvErrors.IsVCenterError(err)).To(BeTrue())
		})

		// Given a VCenterError wrapped with fmt.Errorf
		// When checked with IsVCenterError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("verify: %w", srvErrors.NewVCenterError(errors.New("timeout")))

			// Act & Assert
			Expect(srvErrors.IsVCenterError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsVCenterError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsVCenterError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("ConsoleClientError", func() {
		// Given a ConsoleClientError with status code and message
		// When Error() is called
		// Then it should include both the status code and message
		It("should format message with status code and message", func() {
			// Arrange
			err := srvErrors.NewConsoleClientError(410, "gone")

			// Act & Assert
			Expect(err.Error()).To(Equal("console client error 410: gone"))
		})

		// Given a ConsoleClientError
		// When checked with IsConsoleClientError
		// Then it should return true
		It("should be detected by IsConsoleClientError", func() {
			// Arrange
			err := srvErrors.NewConsoleClientError(401, "unauthorized")

			// Act & Assert
			Expect(srvErrors.IsConsoleClientError(err)).To(BeTrue())
		})

		// Given a ConsoleClientError wrapped with fmt.Errorf
		// When checked with IsConsoleClientError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("dispatch: %w", srvErrors.NewConsoleClientError(401, "unauthorized"))

			// Act & Assert
			Expect(srvErrors.IsConsoleClientError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsConsoleClientError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsConsoleClientError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("InspectorWorkError", func() {
		// Given an InspectorWorkError created with format args
		// When Error() is called
		// Then it should contain the formatted message
		It("should format message with args", func() {
			// Arrange
			err := srvErrors.NewInspectorWorkError("vm %s failed: %s", "vm-1", "snapshot error")

			// Act & Assert
			Expect(err.Error()).To(Equal("vm vm-1 failed: snapshot error"))
		})
	})

	Context("InspectorNotRunningError", func() {
		// Given an InspectorNotRunningError
		// When Error() is called
		// Then it should return the expected message
		It("should format the message", func() {
			// Arrange
			err := srvErrors.NewInspectorNotRunningError()

			// Act & Assert
			Expect(err.Error()).To(Equal("inspector not running"))
		})

		// Given an InspectorNotRunningError
		// When checked with IsInspectorNotRunningError
		// Then it should return true
		It("should be detected by IsInspectorNotRunningError", func() {
			// Act & Assert
			Expect(srvErrors.IsInspectorNotRunningError(srvErrors.NewInspectorNotRunningError())).To(BeTrue())
		})

		// Given an InspectorNotRunningError wrapped with fmt.Errorf
		// When checked with IsInspectorNotRunningError
		// Then it should return true through error chain unwrapping
		It("should be detected when wrapped", func() {
			// Arrange
			wrapped := fmt.Errorf("add vms: %w", srvErrors.NewInspectorNotRunningError())

			// Act & Assert
			Expect(srvErrors.IsInspectorNotRunningError(wrapped)).To(BeTrue())
		})

		// Given a plain error
		// When checked with IsInspectorNotRunningError
		// Then it should return false
		It("should not match unrelated errors", func() {
			// Act & Assert
			Expect(srvErrors.IsInspectorNotRunningError(errors.New("nope"))).To(BeFalse())
		})
	})

	Context("cross-type isolation", func() {
		// Given errors of different types
		// When each Is* function checks the wrong type
		// Then all should return false
		It("should not confuse different error types", func() {
			// Arrange
			notFound := srvErrors.NewResourceNotFoundError("vm", "1")
			duplicate := srvErrors.NewDuplicateResourceError("group", "name", "prod")
			inProgress := srvErrors.NewCollectionInProgressError()
			modeConflict := srvErrors.NewModeConflictError("reason")

			// Act & Assert
			Expect(srvErrors.IsDuplicateResourceError(notFound)).To(BeFalse())
			Expect(srvErrors.IsOperationInProgressError(notFound)).To(BeFalse())
			Expect(srvErrors.IsResourceNotFoundError(duplicate)).To(BeFalse())
			Expect(srvErrors.IsOperationInProgressError(duplicate)).To(BeFalse())
			Expect(srvErrors.IsResourceNotFoundError(inProgress)).To(BeFalse())
			Expect(srvErrors.IsModeConflictError(inProgress)).To(BeFalse())
			Expect(srvErrors.IsResourceNotFoundError(modeConflict)).To(BeFalse())
			Expect(srvErrors.IsOperationInProgressError(modeConflict)).To(BeFalse())
		})
	})
})
