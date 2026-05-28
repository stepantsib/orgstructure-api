// Package validator centralizes payload validation helpers.
//
// Validators return the cleaned-up value (e.g. trimmed) and append to
// the supplied errs.ValidationError on failure, so multiple problems can
// be reported in a single response.
package validator

import (
	"strings"
	"unicode/utf8"

	"orgstructure/internal/errs"
)

const (
	MinNameLen = 1
	MaxNameLen = 200
)

// NonEmptyString validates a 1..MaxNameLen string after trimming surrounding
// whitespace. Returns the trimmed value (empty string on failure).
func NonEmptyString(field, value string, ve *errs.ValidationError) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		ve.Add(field, "must not be empty")
		return ""
	}
	if utf8.RuneCountInString(trimmed) > MaxNameLen {
		ve.Add(field, "must be at most 200 characters")
		return ""
	}
	return trimmed
}
