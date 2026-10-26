// Package cache provides a JSON cache over Redis with prefix invalidation.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Cache stores JSON-encoded values.
type Cache interface {
	// Get decodes the cached value into dest. It returns false on a miss.
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	// InvalidatePrefix deletes every key that starts with prefix.
	InvalidatePrefix(ctx context.Context, prefix string) error
}

type RedisCache struct {
	client    *goredis.Client
	namespace string
}

var _ Cache = (*RedisCache)(nil)

// NewRedisCache namespaces all keys (e.g. "atlas:") to share a Redis safely.
func NewRedisCache(client *goredis.Client, namespace string) *RedisCache {
	return &RedisCache{client: client, namespace: namespace}
}

func (c *RedisCache) key(k string) string { return c.namespace + k }

func (c *RedisCache) Get(ctx context.Context, key string, dest any) (bool, error) {
	raw, err := c.client.Get(ctx, c.key(key)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache get %s: %w", key, err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, fmt.Errorf("cache decode %s: %w", key, err)
	}
	return true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache encode %s: %w", key, err)
	}
	if err := c.client.Set(ctx, c.key(key), raw, ttl).Err(); err != nil {
		return fmt.Errorf("cache set %s: %w", key, err)
	}
	return nil
}

func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = c.key(k)
	}
	return c.client.Del(ctx, full...).Err()
}

func (c *RedisCache) InvalidatePrefix(ctx context.Context, prefix string) error {
	iter := c.client.Scan(ctx, 0, c.key(prefix)+"*", 200).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) == 200 {
			if err := c.client.Unlink(ctx, batch...).Err(); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("cache scan %s: %w", prefix, err)
	}
	if len(batch) > 0 {
		return c.client.Unlink(ctx, batch...).Err()
	}
	return nil
}
