package accesscache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type RedisCache struct {
	client    *goredis.Client
	namespace string
	ttl       time.Duration
}

var _ services.IAccessCache = (*RedisCache)(nil)

func NewRedisCache(client *goredis.Client, namespace string, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, namespace: namespace, ttl: ttl}
}

func (c *RedisCache) Get(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error) {
	raw, err := c.client.Get(ctx, c.key(membershipID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached access: %w", err)
	}
	var access entities.Access
	if err := json.Unmarshal(raw, &access); err != nil {
		return nil, fmt.Errorf("decode cached access: %w", err)
	}
	return &access, nil
}

func (c *RedisCache) Set(ctx context.Context, access *entities.Access) error {
	raw, err := json.Marshal(access)
	if err != nil {
		return fmt.Errorf("encode access: %w", err)
	}
	if err := c.client.Set(ctx, c.key(access.MembershipID), raw, c.ttl).Err(); err != nil {
		return fmt.Errorf("cache access: %w", err)
	}
	return nil
}

func (c *RedisCache) Delete(ctx context.Context, membershipIDs ...uuid.UUID) error {
	if len(membershipIDs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(membershipIDs))
	for _, id := range membershipIDs {
		keys = append(keys, c.key(id))
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("invalidate cached access: %w", err)
	}
	return nil
}

func (c *RedisCache) key(membershipID uuid.UUID) string {
	return c.namespace + "access:" + membershipID.String()
}
