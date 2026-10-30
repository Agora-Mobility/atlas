package mfachallenge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/services"
)

type RedisStore struct {
	client      *goredis.Client
	tokens      services.IOpaqueTokenGenerator
	namespace   string
	ttl         time.Duration
	maxAttempts int64
}

var _ services.IMFAChallengeStore = (*RedisStore)(nil)

func NewRedisStore(client *goredis.Client, tokens services.IOpaqueTokenGenerator, namespace string, ttl time.Duration, maxAttempts int) *RedisStore {
	return &RedisStore{client: client, tokens: tokens, namespace: namespace, ttl: ttl, maxAttempts: int64(maxAttempts)}
}

func (s *RedisStore) Create(ctx context.Context, challenge services.MFAChallenge) (string, error) {
	token, hash, err := s.tokens.Generate()
	if err != nil {
		return "", err
	}
	challenge.ExpiresAt = time.Now().UTC().Add(s.ttl)
	raw, err := json.Marshal(challenge)
	if err != nil {
		return "", fmt.Errorf("encode mfa challenge: %w", err)
	}
	if err := s.client.Set(ctx, s.key(hash), raw, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("store mfa challenge: %w", err)
	}
	return token, nil
}

func (s *RedisStore) Find(ctx context.Context, token string) (*services.MFAChallenge, error) {
	raw, err := s.client.Get(ctx, s.key(s.tokens.Hash(token))).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, services.ErrMFAChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get mfa challenge: %w", err)
	}
	var challenge services.MFAChallenge
	if err := json.Unmarshal(raw, &challenge); err != nil {
		return nil, fmt.Errorf("decode mfa challenge: %w", err)
	}
	return &challenge, nil
}

func (s *RedisStore) RecordFailure(ctx context.Context, token string) (bool, error) {
	hash := s.tokens.Hash(token)
	attemptsKey := s.key(hash) + ":attempts"
	pipe := s.client.TxPipeline()
	incr := pipe.Incr(ctx, attemptsKey)
	pipe.Expire(ctx, attemptsKey, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("record mfa failure: %w", err)
	}
	if incr.Val() < s.maxAttempts {
		return false, nil
	}
	return true, s.client.Del(ctx, s.key(hash), attemptsKey).Err()
}

func (s *RedisStore) Delete(ctx context.Context, token string) error {
	hash := s.tokens.Hash(token)
	if err := s.client.Del(ctx, s.key(hash), s.key(hash)+":attempts").Err(); err != nil {
		return fmt.Errorf("delete mfa challenge: %w", err)
	}
	return nil
}

func (s *RedisStore) key(hash string) string {
	return s.namespace + "mfa-challenge:" + hash
}
