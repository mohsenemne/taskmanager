package observability

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

type loggerKey struct{}

// WithLogger returns a context that carries a request-scoped logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the request-scoped logger, or the default logger when the
// context carries none.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}

// RequestLogger assigns every request an identifier, echoes it in the
// X-Request-ID response header, attaches a request-scoped logger to the context
// and writes one access log line when the request completes.
//
// The log carries both the matched route and the raw path: the route is what
// matches the metrics labels, the path is what tells you which unmatched URL a
// client actually asked for.
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Header("X-Request-ID", requestID)

		requestLogger := logger.With("request_id", requestID)
		// otelgin runs before this middleware, so the span is already in the
		// context. Carrying its identifiers makes the log line resolvable in
		// the trace UI.
		spanContext := trace.SpanContextFromContext(c.Request.Context())
		if spanContext.IsValid() {
			requestLogger = requestLogger.With(
				"trace_id", spanContext.TraceID().String(),
				"span_id", spanContext.SpanID().String(),
			)
		}

		c.Request = c.Request.WithContext(
			WithLogger(c.Request.Context(), requestLogger),
		)

		start := time.Now()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		attrs := []any{
			"method", c.Request.Method,
			"route", route,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", float64(time.Since(start).Microseconds()) / 1000,
			"bytes", c.Writer.Size(),
			"remote_ip", c.ClientIP(),
		}

		switch status := c.Writer.Status(); {
		case status >= 500:
			requestLogger.Error("http request", attrs...)
		case status >= 400:
			requestLogger.Warn("http request", attrs...)
		default:
			requestLogger.Info("http request", attrs...)
		}
	}
}

// Middleware records request counts, latency and in-flight requests.
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		m.RequestsInFlight.Inc()
		defer m.RequestsInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			// Requests that match no route would otherwise each create a new
			// label value and blow up cardinality.
			route = "unmatched"
		}

		status := strconv.Itoa(c.Writer.Status())

		m.RequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()

		elapsed := time.Since(start).Seconds()
		observer := m.RequestDuration.WithLabelValues(c.Request.Method, route)

		// An exemplar attaches the trace that produced this observation to the
		// histogram bucket, so a slow bucket in Grafana can be opened directly
		// in the trace UI instead of searched for.
		spanContext := trace.SpanContextFromContext(c.Request.Context())
		if spanContext.IsValid() {
			exemplarObserver, ok := observer.(prometheus.ExemplarObserver)
			if ok {
				exemplarObserver.ObserveWithExemplar(elapsed, prometheus.Labels{
					"trace_id": spanContext.TraceID().String(),
				})

				return
			}
		}

		observer.Observe(elapsed)
	}
}
