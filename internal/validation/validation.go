package validation

import (
	"fmt"
	"strings"
)

type Error struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func Invalid(fieldName, message string) error {
	return &Error{
		Field:   fieldName,
		Message: message,
	}
}

func Required(fieldName, value string) error {
	if strings.TrimSpace(value) == "" {
		return &Error{
			Field:   fieldName,
			Message: "is required",
		}
	}
	return nil
}

func MaxLength(fieldName, value string, max int) error {
	if len(value) > max {
		return &Error{
			Field:   fieldName,
			Message: fmt.Sprintf("cannot exceed %d characters", max),
		}
	}
	return nil
}

func MinLength(fieldName, value string, min int) error {
	if len(value) < min {
		return &Error{
			Field:   fieldName,
			Message: fmt.Sprintf("must be at least %d characters", min),
		}
	}
	return nil
}