package recoveryticket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const ticketBytes = 32

type RedisRecoveryTicketService struct {
	client    *goredis.Client
	namespace string
	ttl       time.Duration
}

var _ services.IRecoveryTicketService = (*RedisRecoveryTicketService)(nil)

func NewRedisRecoveryTicketService(client *goredis.Client, namespace string, ttl time.Duration) *RedisRecoveryTicketService {
	return &RedisRecoveryTicketService{client: client, namespace: namespace, ttl: ttl}
}

func (s *RedisRecoveryTicketService) Issue(ctx context.Context, subject string) (string, error) {
	raw := make([]byte, ticketBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("recovery ticket: %w", err)
	}
	ticket := hex.EncodeToString(raw)
	if err := s.client.Set(ctx, s.key(ticket), subject, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("recovery ticket: %w", err)
	}
	return ticket, nil
}

func (s *RedisRecoveryTicketService) Peek(ctx context.Context, ticket string) (string, error) {
	return ticketSubject(s.client.Get(ctx, s.key(ticket)).Result())
}

func (s *RedisRecoveryTicketService) Consume(ctx context.Context, ticket string) (string, error) {
	return ticketSubject(s.client.GetDel(ctx, s.key(ticket)).Result())
}

func ticketSubject(subject string, err error) (string, error) {
	if errors.Is(err, goredis.Nil) {
		return "", services.ErrRecoveryTicketInvalid
	}
	if err != nil {
		return "", fmt.Errorf("recovery ticket: %w", err)
	}
	return subject, nil
}

func (s *RedisRecoveryTicketService) key(ticket string) string {
	return s.namespace + "recovery:ticket:" + ticket
}
