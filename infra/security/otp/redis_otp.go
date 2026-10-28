package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	codeDigits     = 6
	verifyAccepted = 1
	verifyRejected = 0
	verifyMissing  = -1
	verifyExceeded = -2
)

var verifyScript = goredis.NewScript(`
local stored = redis.call('HGET', KEYS[1], 'hash')
if not stored then return -1 end
local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if attempts > tonumber(ARGV[2]) then
  redis.call('DEL', KEYS[1])
  return -2
end
if stored == ARGV[1] then
  redis.call('DEL', KEYS[1])
  return 1
end
return 0
`)

type Settings struct {
	TTL            time.Duration
	MaxAttempts    int
	ResendCooldown time.Duration
}

type RedisOTPService struct {
	client    *goredis.Client
	namespace string
	settings  Settings
}

var _ services.IOTPService = (*RedisOTPService)(nil)

func NewRedisOTPService(client *goredis.Client, namespace string, settings Settings) *RedisOTPService {
	return &RedisOTPService{client: client, namespace: namespace, settings: settings}
}

func (s *RedisOTPService) Issue(ctx context.Context, purpose enums.OTPPurpose, phone string) (string, error) {
	if s.settings.ResendCooldown > 0 {
		acquired, err := s.client.SetNX(ctx, s.cooldownKey(purpose, phone), 1, s.settings.ResendCooldown).Result()
		if err != nil {
			return "", fmt.Errorf("otp cooldown: %w", err)
		}
		if !acquired {
			return "", services.ErrOTPCooldown
		}
	}

	code, err := randomCode()
	if err != nil {
		return "", err
	}
	key := s.codeKey(purpose, phone)
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, key)
	pipe.HSet(ctx, key, "hash", hashCode(purpose, phone, code), "attempts", 0)
	pipe.Expire(ctx, key, s.settings.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("store otp: %w", err)
	}
	return code, nil
}

func (s *RedisOTPService) Verify(ctx context.Context, purpose enums.OTPPurpose, phone, code string) error {
	result, err := verifyScript.Run(ctx, s.client,
		[]string{s.codeKey(purpose, phone)},
		hashCode(purpose, phone, code), s.settings.MaxAttempts,
	).Int()
	if err != nil {
		return fmt.Errorf("verify otp: %w", err)
	}

	switch result {
	case verifyAccepted:
		return nil
	case verifyRejected:
		return services.ErrOTPInvalid
	case verifyMissing:
		return services.ErrOTPExpired
	case verifyExceeded:
		return services.ErrOTPMaxAttempts
	default:
		return fmt.Errorf("verify otp: unexpected result %d", result)
	}
}

func (s *RedisOTPService) codeKey(purpose enums.OTPPurpose, phone string) string {
	return fmt.Sprintf("%sotp:%s:%s", s.namespace, purpose, phone)
}

func (s *RedisOTPService) cooldownKey(purpose enums.OTPPurpose, phone string) string {
	return fmt.Sprintf("%sotp-cooldown:%s:%s", s.namespace, purpose, phone)
}

func randomCode() (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(codeDigits), nil)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	return fmt.Sprintf("%0*d", codeDigits, n.Int64()), nil
}

func hashCode(purpose enums.OTPPurpose, phone, code string) string {
	sum := sha256.Sum256([]byte(purpose.String() + ":" + phone + ":" + code))
	return hex.EncodeToString(sum[:])
}
