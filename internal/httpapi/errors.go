package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// Stable error codes callers can branch on, independent of the message text.
const (
	codeInvalidRequest = "invalid_request"
	codeNotFound       = "not_found"
	codeConflict       = "conflict"
	codeInternal       = "internal"
)

// writeError maps a domain error to the one error shape this API returns.
// Unknown errors are logged and reported without leaking their text.
func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, task.ErrInvalid):
		writeJSONError(c, http.StatusBadRequest, codeInvalidRequest, detail(err, task.ErrInvalid))
	case errors.Is(err, task.ErrNotFound):
		writeJSONError(c, http.StatusNotFound, codeNotFound, "task not found")
	case errors.Is(err, task.ErrConflict):
		writeJSONError(c, http.StatusConflict, codeConflict, "task already exists")
	default:
		slog.ErrorContext(c.Request.Context(), "unhandled error", "error", err)
		writeJSONError(c, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// writeBadRequest reports a transport-level problem, such as a malformed body
// or an unparsable query parameter.
func writeBadRequest(c *gin.Context, message string) {
	writeJSONError(c, http.StatusBadRequest, codeInvalidRequest, message)
}

func writeJSONError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorResponse{
		Error: ErrorDetail{Code: code, Message: message},
	})
}

// detail strips the sentinel prefix from a wrapped domain error, so the client
// sees "title is required" rather than "invalid task: title is required".
func detail(err error, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}
