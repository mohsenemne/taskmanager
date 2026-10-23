package observability

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/mohsenemne/taskmanager/internal/app"
)

const cacheTracerName = "github.com/mohsenemne/taskmanager/" +
	"internal/observability"

// instrumentedCache decorates a cache with hit/miss/error counters and a span
// per operation. Doing both in one place keeps the numbers and the traces from
// drifting apart.
type instrumentedCache struct {
	inner   app.Cache
	metrics *Metrics
	timeout time.Duration
}

// NewInstrumentedCache wraps inner so every operation is counted and traced.
// The timeout bounds a slow cache: exceeding it counts as an error and the
// caller falls back to the database.
func NewInstrumentedCache(
	inner app.Cache,
	metrics *Metrics,
	timeout time.Duration,
) app.Cache {
	return &instrumentedCache{inner: inner, metrics: metrics, timeout: timeout}
}

func (c *instrumentedCache) Get(
	ctx context.Context,
	key string,
) ([]byte, bool, error) {
	ctx, span := startCacheSpan(ctx, "cache.get", key)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	value, found, err := c.inner.Get(ctx, key)

	switch {
	case err != nil:
		c.metrics.CacheErrors.WithLabelValues("get").Inc()
		recordCacheError(span, err)
	case found:
		c.metrics.CacheHits.Inc()
		span.SetAttributes(attribute.Bool("cache.hit", true))
	default:
		c.metrics.CacheMisses.Inc()
		span.SetAttributes(attribute.Bool("cache.hit", false))
	}

	return value, found, err
}

func (c *instrumentedCache) Set(
	ctx context.Context,
	key string,
	value []byte,
	ttl time.Duration,
) error {
	ctx, span := startCacheSpan(ctx, "cache.set", key)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.inner.Set(ctx, key, value, ttl); err != nil {
		c.metrics.CacheErrors.WithLabelValues("set").Inc()
		recordCacheError(span, err)

		return err
	}

	return nil
}

func (c *instrumentedCache) Delete(ctx context.Context, keys ...string) error {
	ctx, span := startCacheSpan(ctx, "cache.delete", joinKeys(keys))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if err := c.inner.Delete(ctx, keys...); err != nil {
		c.metrics.CacheErrors.WithLabelValues("delete").Inc()
		recordCacheError(span, err)

		return err
	}

	return nil
}

func startCacheSpan(
	ctx context.Context,
	name string,
	key string,
) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(cacheTracerName).Start(ctx, name)
	span.SetAttributes(attribute.String("cache.key", key))

	return ctx, span
}

func recordCacheError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// joinKeys renders a key list for a span attribute.
func joinKeys(keys []string) string {
	if len(keys) == 1 {
		return keys[0]
	}

	var out strings.Builder

	for i, key := range keys {
		if i > 0 {
			out.WriteString(",")
		}

		out.WriteString(key)
	}

	return out.String()
}
