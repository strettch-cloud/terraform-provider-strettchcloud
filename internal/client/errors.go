package client

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is the single Go type every API failure becomes. The client builds
// it from the error envelope ({ success:false, code, message, requestId }) or
// from a non-2xx status, decides retry vs return, and hands it up to the
// resource layer, which renders it into a Terraform diagnostic.
type APIError struct {
	Status    int    // HTTP status code
	Code      string // UNAUTHORIZED, TOO_MANY_REQUESTS, INTERNAL_SERVER_ERROR, ...
	Message   string // human-readable message from the API
	RequestID string // correlation id; how support finds the request in our logs
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (HTTP %d): %s [requestId=%s]", e.Code, e.Status, e.Message, e.RequestID)
}

// IsNotFound reports whether err is an APIError with HTTP 404. Read uses it to
// turn a missing compute into a state removal instead of an error; everywhere
// else a 404 stays an error.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusNotFound
	}
	return false
}

// isRetryable reports whether a status warrants a retry: 429 and any 5xx.
// 400/401/403/404/422 never retry.
func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}
