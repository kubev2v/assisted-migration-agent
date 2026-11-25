package handlers

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

// tagFormatRegex validates that tags contain only alphanumeric characters, underscores, and dots.
var tagFormatRegex = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)

// RegisterValidators registers custom field-level validators shared across all API versions.
// Called once during application startup before the HTTP server begins serving requests.
func RegisterValidators(v *validator.Validate) {
	_ = v.RegisterValidation("tag_format", validateTagFormat)
	_ = v.RegisterValidation("notblank", validateNotBlank)

	// Validation messages name fields the way clients send them.
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}

		return name
	})
}

// validateTagFormat checks that a tag contains only alphanumeric characters, underscores, and dots.
func validateTagFormat(fl validator.FieldLevel) bool {
	tag := fl.Field().String()
	return tagFormatRegex.MatchString(tag)
}

// validateNotBlank checks that a string is not empty or whitespace-only.
func validateNotBlank(fl validator.FieldLevel) bool {
	value := fl.Field().String()
	return strings.TrimSpace(value) != ""
}
