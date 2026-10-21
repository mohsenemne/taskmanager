package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/mohsenemne/taskmanager/internal/observability"
)

// TestRequestLoggerCarriesTraceAndSpanIDs is the whole point of the tracing
// step: a log line must resolve to the span it was emitted under, otherwise
// logs and traces are two disconnected systems.
func TestRequestLoggerCarriesTraceAndSpanIDs(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	// otelgin must run first so the span exists before the logger is scoped.
	router := newTestRouter(
		otelgin.Middleware("test-service"),
		observability.RequestLogger(logger),
	)
	router.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0].SpanContext

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, span.TraceID().String(), line["trace_id"])
	require.Equal(t, span.SpanID().String(), line["span_id"])
}

func TestNewTracerProviderDefaultsToStdout(t *testing.T) {
	provider, err := observability.NewTracerProvider(context.Background(), observability.TracingInfo{
		ServiceName: "test-service",
	})
	require.NoError(t, err)
	require.NoError(t, provider.Shutdown(context.Background()))
}

func TestNewTracerProviderAcceptsOTLPEndpoint(t *testing.T) {
	provider, err := observability.NewTracerProvider(context.Background(), observability.TracingInfo{
		ServiceName: "test-service",
		Endpoint:    "http://localhost:4318",
	})
	require.NoError(t, err)
	require.NoError(t, provider.Shutdown(context.Background()))
}

func TestNewTracerProviderRejectsInvalidEndpoint(t *testing.T) {
	_, err := observability.NewTracerProvider(context.Background(), observability.TracingInfo{
		Endpoint: "not-a-url",
	})
	require.Error(t, err)
}
