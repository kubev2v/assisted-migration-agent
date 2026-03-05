package handlers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
)

// validationErrorMessage translates validator.ValidationErrors into a
// human-readable message. Falls back to "invalid request body" for
// non-validation errors (e.g. malformed JSON).
func validationErrorMessage(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		msgs := make([]string, 0, len(ve))
		for _, fe := range ve {
			msgs = append(msgs, formatFieldError(fe))
		}
		return strings.Join(msgs, "; ")
	}
	return "invalid request body"
}

func formatFieldError(fe validator.FieldError) string {
	field := fe.Field()
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must not exceed %s characters", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "at_least_one":
		return "at least one field must be provided"
	default:
		return fmt.Sprintf("%s failed validation: %s", field, fe.Tag())
	}
}

// RegisterValidators registers all custom struct-level validators with
// the given validator instance. Called once during application startup.
func RegisterValidators(v *validator.Validate) {
	v.RegisterStructValidation(validateUpdateGroupAtLeastOneField, v1.UpdateGroupRequest{})
}

func validateUpdateGroupAtLeastOneField(sl validator.StructLevel) {
	req := sl.Current().Interface().(v1.UpdateGroupRequest)
	if req.Name == nil && req.Filter == nil && req.Description == nil {
		sl.ReportError(req, "UpdateGroupRequest", "", "at_least_one", "")
	}
}
