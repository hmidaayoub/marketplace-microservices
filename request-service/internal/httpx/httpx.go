// Package httpx holds the JSON response helpers shared by every handler.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// ErrorBody matches the error shape the Java services return, so a client sees one
// error contract across the platform rather than one per language.
type ErrorBody struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged.
		slog.Error("writing response body", "error", err)
	}
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, ErrorBody{Message: message, Status: status})
}

// DecodeJSON reads a request body strictly: unknown fields are rejected so a caller
// cannot smuggle in a field the handler silently ignores (for example a customerId
// that identity must come from the token instead).
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syntax *json.SyntaxError
		switch {
		case errors.As(err, &syntax):
			Error(w, http.StatusBadRequest, "Malformed JSON body")
		case errors.Is(err, io.EOF):
			Error(w, http.StatusBadRequest, "Request body is required")
		default:
			Error(w, http.StatusBadRequest, "Invalid request body")
		}
		return false
	}
	return true
}
