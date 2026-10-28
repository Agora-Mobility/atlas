package customerrors

import (
	"errors"
	"sort"
	"strings"
)

var ErrValidation = errors.New("validation failed")

type ValidationError struct {
	Fields map[string]string
}

func NewValidationError(fields map[string]string) *ValidationError {
	return &ValidationError{Fields: fields}
}

func FieldError(field, message string) *ValidationError {
	return NewValidationError(map[string]string{field: message})
}

func (e *ValidationError) Error() string {
	fields := make([]string, 0, len(e.Fields))
	for field := range e.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return ErrValidation.Error() + ": " + strings.Join(fields, ", ")
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func AsValidation(err error) (*ValidationError, bool) {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr, true
	}
	return nil, false
}
