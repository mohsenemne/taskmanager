package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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
	router.GET("/ping", func(_ *gin.Context) {})

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
	router.GET("/ping", func(_ *gin.Context) {})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "caller-supplied-id")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, "caller-supplied-id", rec.Header().Get("X-Request-ID"))
}

// TestMiddlewareAttachesTraceExemplar proves the latency histogram is linked to
// the trace that produced it, which is what lets a dashboard jump from a slow
// bucket straight to the request.
func TestMiddlewareAttachesTraceExemplar(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	metrics := observability.NewMetrics("test", "test")
	router := newTestRouter(
		otelgin.Middleware("test-service"),
		metrics.Middleware(),
	)
	router.GET("/ping", func(_ *gin.Context) {})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	families, err := metrics.Registry.Gather()
	require.NoError(t, err)

	var found int

	for _, family := range families {
		if family.GetName() != "http_request_duration_seconds" {
			continue
		}

		for _, metric := range family.GetMetric() {
			for _, bucket := range metric.GetHistogram().GetBucket() {
				if bucket.GetExemplar() != nil {
					found++
				}
			}
		}
	}

	require.Positive(t, found, "the histogram should carry an exemplar with the trace id")
}
