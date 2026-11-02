package config_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const secret = "test-secret-with-at-least-32-characters"

var testMFAKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

func isolateEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
}

func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/atlas")
	t.Setenv("REDIS_ADDR", "localhost:6379")
	t.Setenv("RABBITMQ_URL", "amqp://localhost/")
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("MFA_ENCRYPTION_KEY", testMFAKey)
}

func TestLoadDefaults(t *testing.T) {
	isolateEnv(t)
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, enums.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, 8700, cfg.HTTP.Port)
	assert.Equal(t, 15*time.Second, cfg.HTTP.ReadTimeout)
	assert.Equal(t, []string{"*"}, cfg.HTTP.CORSAllowedOrigins)
	assert.Empty(t, cfg.HTTP.TrustedProxies)
	assert.Equal(t, int32(20), cfg.Database.MaxConns)
	assert.Equal(t, 24*time.Hour, cfg.RabbitMQ.DedupTTL)
	assert.Equal(t, secret, cfg.JWT.Secret)
	assert.True(t, cfg.Database.MigrateOnStart)
	assert.False(t, cfg.SMS.Enabled())
	assert.Equal(t, "https://api.castbrick.co/v1", cfg.SMS.BaseURL)
	assert.Equal(t, 10*time.Second, cfg.SMS.Timeout)
	assert.False(t, cfg.Email.Enabled())
	assert.Equal(t, "https://api.resend.com", cfg.Email.BaseURL)
	assert.Equal(t, 10*time.Second, cfg.Email.Timeout)
}

func TestLoadProductionRequiresTheDocumentStorage(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.agora.ao")
	t.Setenv("CASTBRICK_API_KEY", "key")
	t.Setenv("RESEND_API_KEY", "key")
	t.Setenv("EMAIL_FROM", "AGORA <no-reply@agora.ao>")

	_, err := config.Load()
	require.ErrorContains(t, err, "R2_ACCOUNT_ID")

	setAppyPay(t)
	t.Setenv("R2_ACCOUNT_ID", "account")
	t.Setenv("R2_ACCESS_KEY_ID", "id")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
	t.Setenv("R2_BUCKET_NAME", "documents")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "https://account.r2.cloudflarestorage.com", cfg.R2.ResolvedEndpoint())
}

func TestLoadProductionRequiresCastBrickKey(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://agora.ao")

	_, err := config.Load()
	require.ErrorContains(t, err, "CASTBRICK_API_KEY is required in production")

	t.Setenv("CASTBRICK_API_KEY", "cb_live_key")
	t.Setenv("CASTBRICK_SENDER_ID", "AGORA")
	_, err = config.Load()
	require.ErrorContains(t, err, "RESEND_API_KEY is required in production")

	t.Setenv("RESEND_API_KEY", "re_live_key")
	t.Setenv("EMAIL_FROM", "Agora <no-reply@agora.ao>")
	setAppyPay(t)
	t.Setenv("R2_ACCOUNT_ID", "account")
	t.Setenv("R2_ACCESS_KEY_ID", "id")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret")
	t.Setenv("R2_BUCKET_NAME", "documents")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.SMS.Enabled())
	assert.Equal(t, "AGORA", cfg.SMS.SenderID)
}

func TestLoadResendKeyNeedsTheSender(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("RESEND_API_KEY", "re_test_key")

	_, err := config.Load()
	require.ErrorContains(t, err, "EMAIL_FROM is required when RESEND_API_KEY is set")

	t.Setenv("EMAIL_FROM", "Agora <no-reply@agora.ao>")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.Email.Enabled())
	assert.Equal(t, "Agora <no-reply@agora.ao>", cfg.Email.From)
}

func TestLoadEnvOverridesDotEnv(t *testing.T) {
	isolateEnv(t)
	dotEnv := "HTTP_PORT=9000\nLOG_LEVEL=debug\nCORS_ALLOWED_ORIGINS=https://a.ao, ,https://b.ao\n"
	require.NoError(t, os.WriteFile(".env", []byte(dotEnv), 0o600))
	setRequired(t)
	t.Setenv("HTTP_PORT", "7000")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, 7000, cfg.HTTP.Port)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, []string{"https://a.ao", "https://b.ao"}, cfg.HTTP.CORSAllowedOrigins)
}

func TestLoadMissingRequired(t *testing.T) {
	isolateEnv(t)

	_, err := config.Load()

	require.Error(t, err)
	for _, key := range []string{"DATABASE_URL", "REDIS_ADDR", "RABBITMQ_URL", "JWT_SECRET", "MFA_ENCRYPTION_KEY"} {
		assert.ErrorContains(t, err, key)
	}
}

func setAppyPay(t *testing.T) {
	t.Helper()
	for _, key := range []string{"APPYPAY_CLIENT_ID", "APPYPAY_CLIENT_SECRET", "APPYPAY_BASE_URL", "APPYPAY_TOKEN_URL", "APPYPAY_RESOURCE", "APPYPAY_GPO_METHOD", "APPYPAY_REF_METHOD"} {
		t.Setenv(key, "value")
	}
}

func TestLoadProductionRequiresTheAppyPayCredentials(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://agora.ao")

	_, err := config.Load()
	require.ErrorContains(t, err, "APPYPAY_")
}
