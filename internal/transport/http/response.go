// Package http contains the HTTP transport layer: handlers, router, and
// middleware. It depends on the service package and translates between
// JSON / HTTP and the domain layer.
package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"orgstructure/internal/errs"
)

// writeJSON serializes `payload` as JSON with the given status code.
// All response writes funnel through this helper to ensure a consistent
// Content-Type header and to keep error handling in one place.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// errorBody is the shape of every error response. Validation errors include
// a per-field breakdown under `details`.
type errorBody struct {
	Error   string             `json:"error"`
	Code    string             `json:"code,omitempty"`
	Details []errs.FieldError  `json:"details,omitempty"`
}

// writeError maps any service-layer error to the right HTTP status and body.
// Validation errors are unpacked so the client sees individual field issues.
func writeError(w http.ResponseWriter, err error) {
	status := errs.MapHTTPStatus(err)

	var ve *errs.ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, status, errorBody{
			Error:   "validation failed",
			Code:    "validation_error",
			Details: ve.Fields,
		})
		return
	}

	code := ""
	switch status {
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusConflict:
		code = "conflict"
	case http.StatusBadRequest:
		code = "bad_request"
	case http.StatusInternalServerError:
		code = "internal_error"
		// Don't leak internal error details to the client.
		writeJSON(w, status, errorBody{Error: "internal server error", Code: code})
		slog.Error("internal server error", "error", err)
		return
	}

	writeJSON(w, status, errorBody{Error: err.Error(), Code: code})
}

// decodeJSON reads and decodes a JSON body into `dst`. Anything other than a
// well-formed object becomes a 400.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errs.ErrBadRequest
	}
	return nil
}
