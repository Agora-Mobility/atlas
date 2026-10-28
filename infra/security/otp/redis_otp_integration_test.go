//go:build integration

package otp_test

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/security/otp"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const (
	redisImage = "redis:8.8.4-alpine"
	phone      = "+244923000000"
)

func setupRedis(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)
	client := goredis.NewClient(&goredis.Options{Addr: endpoint})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisOTPService(t *testing.T) {
	client := setupRedis(t)
	ctx := context.Background()
	settings := otp.Settings{TTL: time.Minute, MaxAttempts: 3, ResendCooldown: time.Minute}

	t.Run("accepts the issued code once", func(t *testing.T) {
		service := otp.NewRedisOTPService(client, "once:", settings)
		code, err := service.Issue(ctx, enums.OTPPurposeVerifyPhone, phone)
		require.NoError(t, err)
		assert.Len(t, code, 6)

		require.NoError(t, service.Verify(ctx, enums.OTPPurposeVerifyPhone, phone, code))
		require.ErrorIs(t, service.Verify(ctx, enums.OTPPurposeVerifyPhone, phone, code), services.ErrOTPExpired)
	})

	t.Run("codes are bound to their purpose", func(t *testing.T) {
		service := otp.NewRedisOTPService(client, "purpose:", settings)
		code, err := service.Issue(ctx, enums.OTPPurposeVerifyPhone, phone)
		require.NoError(t, err)

		require.ErrorIs(t, service.Verify(ctx, enums.OTPPurposeResetPassword, phone, code), services.ErrOTPExpired)
	})

	t.Run("resend is throttled by the cooldown", func(t *testing.T) {
		service := otp.NewRedisOTPService(client, "cooldown:", settings)
		_, err := service.Issue(ctx, enums.OTPPurposeVerifyPhone, phone)
		require.NoError(t, err)

		_, err = service.Issue(ctx, enums.OTPPurposeVerifyPhone, phone)
		require.ErrorIs(t, err, services.ErrOTPCooldown)
	})

	t.Run("too many wrong attempts burn the code", func(t *testing.T) {
		service := otp.NewRedisOTPService(client, "attempts:", settings)
		code, err := service.Issue(ctx, enums.OTPPurposeVerifyPhone, phone)
		require.NoError(t, err)

		for range settings.MaxAttempts {
			require.ErrorIs(t, service.Verify(ctx, enums.OTPPurposeVerifyPhone, phone, "000000x"), services.ErrOTPInvalid)
		}
		require.ErrorIs(t, service.Verify(ctx, enums.OTPPurposeVerifyPhone, phone, code), services.ErrOTPMaxAttempts)
	})
}
