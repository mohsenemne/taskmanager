package observability_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/mohsenemne/taskmanager/internal/observability"
)

// newTestRouter builds a gin engine in release mode. Setting the mode here
// rather than in TestMain keeps each test file self-contained: gin's mode is
// process-global, and the first router built already silences the warnings.
func newTestRouter(middleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(middleware...)
	return router
}

func TestMiddlewareRecordsRequest(t *testing.T) {
	metrics := observability.NewMetrics("test", "test")

	router := newTestRouter(metrics.Middleware())
	router.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.RequestsTotal.WithLabelValues(http.MethodGet, "/ping", "200")))
	require.Equal(t, float64(0), testutil.ToFloat64(metrics.RequestsInFlight))
}

func TestMiddlewareLabelsUnmatchedRoutes(t *testing.T) {
	metrics := observability.NewMetrics("test", "test")

	router := newTestRouter(metrics.Middleware())
	router.GET("/ping", func(c *gin.Context) {})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope/123", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)

	// The raw path must never become a label value, or a client could create
	// unbounded series just by requesting random URLs.
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.RequestsTotal.WithLabelValues(http.MethodGet, "unmatched", "404")))
}

func TestRequestLoggerCarriesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	router := newTestRouter(observability.RequestLogger(logger))
	router.GET("/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))

	requestID := rec.Header().Get("X-Request-ID")
	require.NotEmpty(t, requestID)

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, requestID, line["request_id"])
	require.Equal(t, float64(http.StatusNoContent), line["status"])
	require.Equal(t, http.MethodGet, line["method"])
	require.Equal(t, "/ping", line["route"])
}

func TestRequestLoggerReusesIncomingRequestID(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	router := newTestRouter(observability.RequestLogger(logger))
	router.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "caller-supplied-id")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, "caller-supplied-id", rec.Header().Get("X-Request-ID"))
}
