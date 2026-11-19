package config_test

import (
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
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/atlas")
	t.Setenv("REDIS_ADDR", "localhost:6379")
	t.Setenv("RABBITMQ_URL", "amqp://localhost/")
	t.Setenv("JWT_SECRET", secret)
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
	assert.Equal(t, 7*time.Second, cfg.Dispatch.CadenceApproachInterval)
	assert.Equal(t, 30, cfg.Dispatch.CadenceApproachDistanceM)
	assert.Equal(t, 10*time.Second, cfg.Dispatch.CadenceTripInterval)
	assert.Equal(t, 2, cfg.Dispatch.CadenceSlowdownFactor)
}

func TestLoadRefusesACadenceTooFast(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("CADENCE_APPROACH_INTERVAL", "1s")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CADENCE_APPROACH_INTERVAL")
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
	for _, key := range []string{"DATABASE_URL", "REDIS_ADDR", "RABBITMQ_URL", "JWT_SECRET"} {
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

func TestLoadReadsTheFCMCredentialsFromAFile(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	path := t.TempDir() + "/firebase.json"
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"service_account"}`), 0o600))
	t.Setenv("FCM_PROJECT_ID", "agora")
	t.Setenv("FCM_CREDENTIALS_JSON", `{"inline":true}`)
	t.Setenv("FCM_CREDENTIALS_FILE", path)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"service_account"}`, cfg.FCM.CredentialsJSON, "the file wins over the inline JSON")
	assert.True(t, cfg.FCM.Enabled())
}

func TestLoadFailsWhenTheFCMCredentialsFileIsMissing(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("FCM_CREDENTIALS_FILE", "/nowhere/firebase.json")

	_, err := config.Load()
	require.ErrorContains(t, err, "FCM_CREDENTIALS_FILE")
}

func TestLoadReadsTheAPNsKeyFromAFile(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	path := t.TempDir() + "/apns.p8"
	key := "contents of the p8 file"
	require.NoError(t, os.WriteFile(path, []byte(key), 0o600))
	t.Setenv("APNS_KEY_ID", "KEY")
	t.Setenv("APNS_TEAM_ID", "TEAM")
	t.Setenv("APNS_BUNDLE_ID", "com.agora.iris")
	t.Setenv("APNS_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("not this")))
	t.Setenv("APNS_PRIVATE_KEY_FILE", path)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.APNS.Enabled())
	pem, err := cfg.APNS.KeyPEM()
	require.NoError(t, err)
	assert.Equal(t, key, string(pem), "the file wins over the base64")
}

func TestLoadReadsTheAPNsKeyFromBase64(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("APNS_KEY_ID", "KEY")
	t.Setenv("APNS_TEAM_ID", "TEAM")
	t.Setenv("APNS_BUNDLE_ID", "com.agora.iris")
	t.Setenv("APNS_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("key")))

	cfg, err := config.Load()
	require.NoError(t, err)
	pem, err := cfg.APNS.KeyPEM()
	require.NoError(t, err)
	assert.Equal(t, "key", string(pem))
}

func TestAPNsIsOffWithoutAKey(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("APNS_KEY_ID", "KEY")
	t.Setenv("APNS_TEAM_ID", "TEAM")
	t.Setenv("APNS_BUNDLE_ID", "com.agora.iris")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.APNS.Enabled())
}

func TestLoadFailsWhenTheAPNsKeyFileIsMissing(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("APNS_PRIVATE_KEY_FILE", "/nowhere/apns.p8")

	_, err := config.Load()
	require.ErrorContains(t, err, "APNS_PRIVATE_KEY_FILE")
}

func TestLoadHeatmapDefaults(t *testing.T) {
	isolateEnv(t)
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, cfg.Heatmap.Interval)
	assert.Equal(t, 15*time.Minute, cfg.Heatmap.Window)
	assert.Equal(t, []int{8, 7, 6}, cfg.Heatmap.Rollups)
	assert.Equal(t, []config.HeatmapZoom{{MinZoom: 0, Resolution: 6}, {MinZoom: 11, Resolution: 7}, {MinZoom: 13, Resolution: 8}, {MinZoom: 15, Resolution: 9}}, cfg.Heatmap.Zooms)
	assert.InDelta(t, 0.6, cfg.Heatmap.SelfWeight, 1e-9)
	assert.InDelta(t, 0.4, cfg.Heatmap.NeighborWeight, 1e-9)
	assert.Equal(t, 20_000, cfg.Heatmap.MaxCells)
	assert.Equal(t, 7*24*time.Hour, cfg.Heatmap.MaxPeriod)
}

func TestLoadRejectsAHeatmapZoomOnAResolutionNotStored(t *testing.T) {
	isolateEnv(t)
	setRequired(t)
	t.Setenv("HEATMAP_ROLLUP_RESOLUTIONS", "8,7")
	t.Setenv("HEATMAP_WINDOW", "7m")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HEATMAP_ZOOM_RESOLUTIONS")
	assert.Contains(t, err.Error(), "HEATMAP_WINDOW must divide a day")
}
