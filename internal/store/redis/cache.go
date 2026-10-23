// Package redis implements the cache port on top of Redis.
package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Cache is the Redis implementation of app.Cache.
type Cache struct {
	client goredis.Cmdable
}

// New returns a cache backed by client.
func New(client goredis.Cmdable) *Cache {
	return &Cache{client: client}
}

// Open returns a client for addr.
func Open(addr string) *goredis.Client {
	return goredis.NewClient(&goredis.Options{Addr: addr})
}

// Get returns the cached value. A missing key is (nil, false, nil), not an
// error, so the use cases can treat it as an ordinary miss.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	return value, true, nil
}

// Set stores value under key for at most ttl.
func (c *Cache) Set(
	ctx context.Context,
	key string,
	value []byte,
	ttl time.Duration,
) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes the given keys.
func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	return c.client.Del(ctx, keys...).Err()
}
