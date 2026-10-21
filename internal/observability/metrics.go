// Package observability contains the service's logging, metrics and the HTTP
// middleware that connects them to requests.
package observability

import (
	"context"
	"net/http"
	"time"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns the Prometheus registry and the application instruments.
//
// A dedicated registry is used instead of the global default one so the
// service exposes exactly what it registers and nothing a dependency happens
// to add.
type Metrics struct {
	Registry *prometheus.Registry

	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge
	TasksCount       prometheus.Gauge
}

// NewMetrics builds the registry, the application instruments and the runtime
// and process collectors.
func NewMetrics(version, environment string) *Metrics {
	m := &Metrics{
		Registry: prometheus.NewRegistry(),

		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests by method, route and status code.",
		}, []string{"method", "route", "status"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request latency in seconds.",
			// Tuned for a low-latency API: the defaults stop at 10s and are too
			// coarse below 100ms, where this service actually lives.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"method", "route"}),

		RequestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Number of HTTP requests currently being served.",
		}),

		TasksCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "tasks_count",
			Help: "Number of tasks currently stored.",
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "app_build_info",
		Help: "Build information; the value is always 1.",
	}, []string{"version", "environment"})
	buildInfo.WithLabelValues(version, environment).Set(1)

	m.Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.RequestsTotal,
		m.RequestDuration,
		m.RequestsInFlight,
		m.TasksCount,
		buildInfo,
	)

	return m
}

// Handler serves the registry in the Prometheus text format. OpenMetrics is
// enabled because exemplars, which carry the trace ID, are only rendered there.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// PoolStats is a snapshot of the database connection pool.
type PoolStats struct {
	Acquired int32
	Idle     int32
	Total    int32
	Max      int32
}

// RegisterPoolMetrics exposes connection pool occupancy at scrape time. The
// numbers come from in-memory counters, so the callbacks are cheap.
func RegisterPoolMetrics(reg prometheus.Registerer, stats func() PoolStats) {
	reg.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_acquired_conns",
			Help: "Connections currently checked out of the pool.",
		}, func() float64 { return float64(stats().Acquired) }),

		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_idle_conns",
			Help: "Connections currently idle in the pool.",
		}, func() float64 { return float64(stats().Idle) }),

		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_total_conns",
			Help: "Total connections currently in the pool.",
		}, func() float64 { return float64(stats().Total) }),

		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "db_pool_max_conns",
			Help: "Configured maximum number of connections.",
		}, func() float64 { return float64(stats().Max) }),
	)
}

// StartTaskCount refreshes gauge with the stored task count on an interval
// until ctx is cancelled. Counting on a schedule keeps the gauge truthful even
// when other writers change the table; the cost is one cheap query per tick and
// up to one interval of staleness.
func StartTaskCount(
	ctx context.Context,
	interval time.Duration,
	count func(context.Context) (int64, error),
	gauge prometheus.Gauge,
	logger *slog.Logger,
) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			if n, err := count(ctx); err != nil {
				logger.Warn("count tasks", "error", err)
			} else {
				gauge.Set(float64(n))
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
