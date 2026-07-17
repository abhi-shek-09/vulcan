package validation

import (
	"fmt"
	"strings"
)

func Required(fieldName, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", fieldName)
	}

	return nil
}

func MaxLength(fieldName, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s cannot exceed %d characters", fieldName, max)
	}

	return nil
}

func MinLength(fieldName, value string, min int) error {
	if len(value) < min {
		return fmt.Errorf("%s must be at least %d characters", fieldName, min)
	}

	return nil
}
