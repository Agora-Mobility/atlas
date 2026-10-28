package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

const secret = "test-secret-with-at-least-32-characters"

func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/atlas")
	t.Setenv("REDIS_ADDR", "localhost:6379")
	t.Setenv("RABBITMQ_URL", "amqp://localhost/")
	t.Setenv("JWT_SECRET", secret)
}

func TestLoadDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, enums.EnvDevelopment, cfg.App.Env)
	assert.Equal(t, 8080, cfg.HTTP.Port)
	assert.Equal(t, 15*time.Second, cfg.HTTP.ReadTimeout)
	assert.Equal(t, []string{"*"}, cfg.HTTP.CORSAllowedOrigins)
	assert.Empty(t, cfg.HTTP.TrustedProxies)
	assert.Equal(t, int32(20), cfg.Database.MaxConns)
	assert.Equal(t, 24*time.Hour, cfg.RabbitMQ.DedupTTL)
	assert.Equal(t, secret, cfg.JWT.Secret)
}

func TestLoadEnvOverridesDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
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
	t.Chdir(t.TempDir())

	_, err := config.Load()

	require.Error(t, err)
	for _, key := range []string{"DATABASE_URL", "REDIS_ADDR", "RABBITMQ_URL", "JWT_SECRET"} {
		assert.ErrorContains(t, err, key)
	}
}
