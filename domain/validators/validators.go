// Package validators collects field errors for entity validation.
package validators

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

// Errors accumulates field-level validation messages.
type Errors map[string]string

func (e Errors) Add(field, message string) {
	if _, exists := e[field]; !exists {
		e[field] = message
	}
}

// Required adds an error when value is blank.
func (e Errors) Required(field, value string) {
	if strings.TrimSpace(value) == "" {
		e.Add(field, "is required")
	}
}

// MaxLength adds an error when value has more than maxLen characters.
func (e Errors) MaxLength(field, value string, maxLen int) {
	if utf8.RuneCountInString(value) > maxLen {
		e.Add(field, fmt.Sprintf("must have at most %d characters", maxLen))
	}
}

// Err returns a validation AppError, or nil when there are no errors.
func (e Errors) Err() error {
	if len(e) == 0 {
		return nil
	}
	return customerrors.Validation("validation failed", e)
}
