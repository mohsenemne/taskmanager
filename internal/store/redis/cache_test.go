package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	redisstore "github.com/mohsenemne/taskmanager/internal/store/redis"
)

// TestCacheRoundTrip covers what a mock cannot: that a missing key becomes a
// miss rather than an error, and that values survive a round trip unchanged.
func TestCacheRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	ctx := context.Background()

	container, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "redis:7-alpine",
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor: wait.ForLog("Ready to accept connections").
					WithStartupTimeout(30 * time.Second),
			},
			Started: true,
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	addr, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	client := redisstore.Open(addr)

	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(ctx).Err())

	cache := redisstore.New(client)

	_, found, err := cache.Get(ctx, "missing")
	require.NoError(t, err, "a missing key must not be an error")
	require.False(t, found)

	require.NoError(t, cache.Set(ctx, "present", []byte("value"), time.Minute))

	value, found, err := cache.Get(ctx, "present")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("value"), value)

	require.NoError(t, cache.Delete(ctx, "present"))

	_, found, err = cache.Get(ctx, "present")
	require.NoError(t, err)
	require.False(t, found)
}
