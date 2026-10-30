// Package revocation keeps the ids of revoked sessions in Redis until their
// access tokens expire.
package revocation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/services"
)

type RedisStore struct {
	client    *goredis.Client
	namespace string
	// ttl is the access token lifetime: after it, the token is expired anyway.
	ttl time.Duration
}

var _ services.ISessionRevocationStore = (*RedisStore)(nil)

func NewRedisStore(client *goredis.Client, namespace string, accessTokenTTL time.Duration) *RedisStore {
	return &RedisStore{client: client, namespace: namespace, ttl: accessTokenTTL}
}

func (s *RedisStore) RevokeSessions(ctx context.Context, sessionIDs ...uuid.UUID) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, id := range sessionIDs {
		pipe.Set(ctx, s.key(id), 1, s.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	return nil
}

func (s *RedisStore) IsSessionRevoked(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	n, err := s.client.Exists(ctx, s.key(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("check revoked session: %w", err)
	}
	return n > 0, nil
}

func (s *RedisStore) key(sessionID uuid.UUID) string {
	return s.namespace + "revoked-session:" + sessionID.String()
}
