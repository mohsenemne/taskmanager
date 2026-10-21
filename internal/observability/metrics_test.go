package observability_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/mohsenemne/taskmanager/internal/observability"
)

// TestNewMetricsExposesRequiredSeries guards the registry wiring: it is easy to
// build instruments and forget to register them, or to drop a collector during
// a refactor without any test noticing.
func TestNewMetricsExposesRequiredSeries(t *testing.T) {
	metrics := observability.NewMetrics("v1", "test")

	families, err := metrics.Registry.Gather()
	require.NoError(t, err)

	names := make(map[string]bool, len(families))
	for _, family := range families {
		names[family.GetName()] = true
	}

	// http_requests_total is absent until the first request, because
	// CounterVec children are created lazily.
	for _, want := range []string{
		"app_build_info",
		"tasks_count",
		"go_goroutines",
		"go_memstats_heap_alloc_bytes",
		"process_resident_memory_bytes",
		"process_cpu_seconds_total",
	} {
		require.True(t, names[want], "registry is missing %s", want)
	}
}

func TestStartTaskCountUpdatesGauge(t *testing.T) {
	metrics := observability.NewMetrics("test", "test")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	observability.StartTaskCount(ctx, 5*time.Millisecond, func(context.Context) (int64, error) {
		return 42, nil
	}, metrics.TasksCount, logger)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(metrics.TasksCount) == 42
	}, time.Second, 5*time.Millisecond)
}
