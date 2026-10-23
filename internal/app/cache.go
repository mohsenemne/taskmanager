package app

import (
	"context"
	"time"
)

// Cache is the read-through store used for hot reads. A missing key is
// reported as found=false with a nil error, so callers never compare
// sentinels.
type Cache interface {
	// Get returns the cached value and whether it was found.
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	// Set stores value under key for at most ttl.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes the given keys.
	Delete(ctx context.Context, keys ...string) error
}
