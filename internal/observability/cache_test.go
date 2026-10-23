package observability_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/mohsenemne/taskmanager/internal/app/mocks"
	"github.com/mohsenemne/taskmanager/internal/observability"
)

func newInstrumentedCache(t *testing.T) (*mocks.MockCache, *observability.Metrics) {
	t.Helper()

	metrics := observability.NewMetrics("test", "test")
	inner := mocks.NewMockCache(t)

	return inner, metrics
}

func TestInstrumentedCacheCountsHits(t *testing.T) {
	inner, metrics := newInstrumentedCache(t)
	inner.EXPECT().
		Get(mock.Anything, "key").
		Return([]byte("value"), true, nil)

	cache := observability.NewInstrumentedCache(inner, metrics, time.Second)

	value, found, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("value"), value)

	require.Equal(t, float64(1), testutil.ToFloat64(metrics.CacheHits))
	require.Equal(t, float64(0), testutil.ToFloat64(metrics.CacheMisses))
}

func TestInstrumentedCacheCountsMisses(t *testing.T) {
	inner, metrics := newInstrumentedCache(t)
	inner.EXPECT().
		Get(mock.Anything, "key").
		Return(nil, false, nil)

	cache := observability.NewInstrumentedCache(inner, metrics, time.Second)

	_, found, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)
	require.False(t, found)

	require.Equal(t, float64(0), testutil.ToFloat64(metrics.CacheHits))
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.CacheMisses))
}

func TestInstrumentedCacheCountsErrors(t *testing.T) {
	inner, metrics := newInstrumentedCache(t)
	inner.EXPECT().
		Get(mock.Anything, "key").
		Return(nil, false, errors.New("connection refused"))

	cache := observability.NewInstrumentedCache(inner, metrics, time.Second)

	_, _, err := cache.Get(context.Background(), "key")
	require.Error(t, err)

	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.CacheErrors.WithLabelValues("get")))
}

func TestInstrumentedCacheCountsSetAndDeleteErrors(t *testing.T) {
	inner, metrics := newInstrumentedCache(t)
	inner.EXPECT().
		Set(mock.Anything, "key", mock.Anything, time.Minute).
		Return(errors.New("boom"))
	inner.EXPECT().
		Delete(mock.Anything, "key").
		Return(errors.New("boom"))

	cache := observability.NewInstrumentedCache(inner, metrics, time.Second)

	require.Error(t, cache.Set(context.Background(), "key", []byte("v"), time.Minute))
	require.Error(t, cache.Delete(context.Background(), "key"))

	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.CacheErrors.WithLabelValues("set")))
	require.Equal(t, float64(1), testutil.ToFloat64(
		metrics.CacheErrors.WithLabelValues("delete")))
}

// TestInstrumentedCacheTracesTheOperation is the "separate execution" part:
// cache time must appear as its own span rather than being folded into the
// database call.
func TestInstrumentedCacheTracesTheOperation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	inner, metrics := newInstrumentedCache(t)
	inner.EXPECT().
		Get(mock.Anything, "key").
		Return(nil, false, nil)

	cache := observability.NewInstrumentedCache(inner, metrics, time.Second)
	_, _, err := cache.Get(context.Background(), "key")
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "cache.get", spans[0].Name)

	attrs := make(map[string]any, len(spans[0].Attributes))

	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}

	require.Equal(t, false, attrs["cache.hit"])
}
