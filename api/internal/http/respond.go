package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// APIError is an error with an HTTP status attached, so handlers can return a
// single error value and let fail() pick the right status.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

func errorf(status int, msg string) *APIError {
	return &APIError{Status: status, Message: msg}
}

var (
	errUnauthorized = errorf(http.StatusUnauthorized, "not authenticated")
	errNotFound     = errorf(http.StatusNotFound, "not found")
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "error", err)
	}
}

// fail writes err as a JSON error envelope. Unrecognised errors become a 500
// with a generic message so internals never leak to the client.
func fail(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Message})
		return
	}
	slog.Error("unhandled request error", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

// decode reads a JSON body into dst, rejecting unknown fields.
func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errorf(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	return nil
}
