// Package httpserver — RFC 7807 error response helpers.
package httpserver

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the JSON body for error responses.
// Aligns with RFC 7807 ProblemDetails.
type ErrorResponse struct {
	Status  int    `json:"status"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

// WriteError writes an RFC-7807-style JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := ErrorResponse{
		Status:  status,
		Error:   http.StatusText(status),
		Message: message,
	}
	_ = json.NewEncoder(w).Encode(resp)
}
