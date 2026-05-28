// Package errs defines the typed sentinel errors used across layers.
//
// Each layer returns these errors instead of HTTP codes or raw GORM errors,
// keeping business logic free of transport concerns. The HTTP layer maps
// them to status codes via MapHTTPStatus.
package errs

import (
	"errors"
	"net/http"
)

var (
	// ErrNotFound is returned when a referenced entity does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict covers cycle creation, duplicate names within the same parent,
	// and similar logical conflicts.
	ErrConflict = errors.New("conflict")

	// ErrValidation signals that the caller's payload failed validation.
	// Callers should usually return a ValidationError (which wraps this).
	ErrValidation = errors.New("validation failed")

	// ErrBadRequest covers malformed JSON, bad query params, etc.
	ErrBadRequest = errors.New("bad request")
)

// FieldError describes a single field-level problem.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError is a collection of field errors; it implements `error`
// and wraps ErrValidation so callers can `errors.Is(err, ErrValidation)`.
type ValidationError struct {
	Fields []FieldError `json:"errors"`
}

func (v *ValidationError) Error() string { return ErrValidation.Error() }
func (v *ValidationError) Unwrap() error { return ErrValidation }

func (v *ValidationError) Add(field, message string) {
	v.Fields = append(v.Fields, FieldError{Field: field, Message: message})
}

func (v *ValidationError) HasErrors() bool { return len(v.Fields) > 0 }

// NewValidation returns a fresh, empty ValidationError ready to accumulate fields.
func NewValidation() *ValidationError { return &ValidationError{} }

// MapHTTPStatus converts an error to its HTTP status code.
// Unknown errors map to 500.
func MapHTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrValidation):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
