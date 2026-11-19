// Package config loads and validates the application configuration from
// environment variables with Viper. Every setting comes from the environment;
// a .env file is only a convenience for local development.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Config struct {
	App        AppConfig        `mapstructure:",squash"`
	HTTP       HTTPConfig       `mapstructure:",squash"`
	Log        LogConfig        `mapstructure:",squash"`
	Telemetry  TelemetryConfig  `mapstructure:",squash"`
	Database   DatabaseConfig   `mapstructure:",squash"`
	Redis      RedisConfig      `mapstructure:",squash"`
	RabbitMQ   RabbitMQConfig   `mapstructure:",squash"`
	Outbox     OutboxConfig     `mapstructure:",squash"`
	JWT        JWTConfig        `mapstructure:",squash"`
	OTP        OTPConfig        `mapstructure:",squash"`
	SMS        SMSConfig        `mapstructure:",squash"`
	Email      EmailConfig      `mapstructure:",squash"`
	Access     AccessConfig     `mapstructure:",squash"`
	Dispatch   DispatchConfig   `mapstructure:",squash"`
	Realtime   RealtimeConfig   `mapstructure:",squash"`
	GoogleMaps GoogleMapsConfig `mapstructure:",squash"`
	FCM        FCMConfig        `mapstructure:",squash"`
	APNS       APNSConfig       `mapstructure:",squash"`
	R2         R2Config         `mapstructure:",squash"`
	Verify     VerifyConfig     `mapstructure:",squash"`
	AppyPay    AppyPayConfig    `mapstructure:",squash"`
	Calls      CallsConfig      `mapstructure:",squash"`
	Heatmap    HeatmapConfig    `mapstructure:",squash"`
}

type CallsConfig struct {
	BaseURL string        `mapstructure:"CALLS_BASE_URL"`
	APIKey  string        `mapstructure:"CALLS_API_KEY"`
	Timeout time.Duration `mapstructure:"CALLS_TIMEOUT"`
}

func (c CallsConfig) Enabled() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.APIKey) != ""
}

type AppyPayConfig struct {
	ClientID               string        `mapstructure:"APPYPAY_CLIENT_ID"`
	ClientSecret           string        `mapstructure:"APPYPAY_CLIENT_SECRET"`
	BaseURL                string        `mapstructure:"APPYPAY_BASE_URL"`
	TokenURL               string        `mapstructure:"APPYPAY_TOKEN_URL"`
	Resource               string        `mapstructure:"APPYPAY_RESOURCE"`
	GPOMethod              string        `mapstructure:"APPYPAY_GPO_METHOD"`
	REFMethod              string        `mapstructure:"APPYPAY_REF_METHOD"`
	Timeout                time.Duration `mapstructure:"APPYPAY_TIMEOUT"`
	GPOWindow              time.Duration `mapstructure:"PAYMENT_GPO_WINDOW"`
	ReferenceTTL           time.Duration `mapstructure:"PAYMENT_REFERENCE_TTL"`
	ReconcileInterval      time.Duration `mapstructure:"PAYMENT_RECONCILE_INTERVAL"`
	ReconcileBatch         int           `mapstructure:"PAYMENT_RECONCILE_BATCH"`
	GPOCheckInterval       time.Duration `mapstructure:"PAYMENT_GPO_CHECK_INTERVAL"`
	ReferenceCheckInterval time.Duration `mapstructure:"PAYMENT_REFERENCE_CHECK_INTERVAL"`
	LedgerCurrency         string        `mapstructure:"LEDGER_CURRENCY"`
}

func (c AppyPayConfig) Enabled() bool {
	for _, value := range []string{c.ClientID, c.ClientSecret, c.BaseURL, c.TokenURL, c.Resource, c.GPOMethod, c.REFMethod} {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

type R2Config struct {
	AccountID       string        `mapstructure:"R2_ACCOUNT_ID"`
	AccessKeyID     string        `mapstructure:"R2_ACCESS_KEY_ID"`
	SecretAccessKey string        `mapstructure:"R2_SECRET_ACCESS_KEY"`
	Bucket          string        `mapstructure:"R2_BUCKET_NAME"`
	Endpoint        string        `mapstructure:"R2_ENDPOINT"`
	PresignTTL      time.Duration `mapstructure:"R2_PRESIGN_TTL"`
}

func (c R2Config) Enabled() bool {
	return c.ResolvedEndpoint() != "" && strings.TrimSpace(c.AccessKeyID) != "" && strings.TrimSpace(c.SecretAccessKey) != "" && strings.TrimSpace(c.Bucket) != ""
}

func (c R2Config) ResolvedEndpoint() string {
	if endpoint := strings.TrimSpace(c.Endpoint); endpoint != "" {
		return endpoint
	}
	if account := strings.TrimSpace(c.AccountID); account != "" {
		return "https://" + account + ".r2.cloudflarestorage.com"
	}
	return ""
}

type VerifyConfig struct {
	MaxFileBytes          int64         `mapstructure:"VERIFICATION_MAX_FILE_BYTES"`
	ReadURLTTL            time.Duration `mapstructure:"VERIFICATION_READ_URL_TTL"`
	MinProviderAge        int           `mapstructure:"VERIFICATION_MIN_PROVIDER_AGE"`
	MaxAttempts           int           `mapstructure:"VERIFICATION_MAX_ATTEMPTS"`
	CriminalRecordMaxDays int           `mapstructure:"VERIFICATION_CRIMINAL_RECORD_MAX_DAYS"`
	CertificateMaxMonths  int           `mapstructure:"VERIFICATION_CERTIFICATE_MAX_MONTHS"`
	ExpiryNoticeDays      int           `mapstructure:"VERIFICATION_EXPIRY_NOTICE_DAYS"`
	PendingUploadTTL      time.Duration `mapstructure:"VERIFICATION_PENDING_UPLOAD_TTL"`
	ReminderDelay         time.Duration `mapstructure:"VERIFICATION_REMINDER_DELAY"`
	HousekeepingInterval  time.Duration `mapstructure:"VERIFICATION_HOUSEKEEPING_INTERVAL"`
	UploadRateLimit       int           `mapstructure:"VERIFICATION_UPLOAD_RATE_LIMIT"`
	CheckRateLimit        int           `mapstructure:"VERIFICATION_CHECK_RATE_LIMIT"`
	RateWindow            time.Duration `mapstructure:"VERIFICATION_RATE_WINDOW"`
	QualityProvider       string        `mapstructure:"VERIFICATION_QUALITY_PROVIDER"`
	OCRProvider           string        `mapstructure:"VERIFICATION_OCR_PROVIDER"`
	LivenessProvider      string        `mapstructure:"VERIFICATION_LIVENESS_PROVIDER"`
	FakeQualityReasons    []string      `mapstructure:"VERIFICATION_FAKE_QUALITY_REASONS"`
	FakeOCRFail           bool          `mapstructure:"VERIFICATION_FAKE_OCR_FAIL"`
	FakeLivenessFail      bool          `mapstructure:"VERIFICATION_FAKE_LIVENESS_FAIL"`
	Workers               int           `mapstructure:"VERIFICATION_WORKERS"`
}

var verificationModes = map[string]bool{"fake": true, "manual": true}

func (c VerifyConfig) validate(env enums.Environment) []error {
	var errs []error
	if c.MaxFileBytes <= 0 || c.ReadURLTTL <= 0 || c.MaxAttempts <= 0 || c.MinProviderAge <= 0 || c.Workers <= 0 {
		errs = append(errs, errors.New("VERIFICATION_MAX_FILE_BYTES, VERIFICATION_READ_URL_TTL, VERIFICATION_MAX_ATTEMPTS, VERIFICATION_MIN_PROVIDER_AGE and VERIFICATION_WORKERS must be positive"))
	}
	if c.CriminalRecordMaxDays <= 0 || c.CertificateMaxMonths <= 0 || c.ExpiryNoticeDays <= 0 {
		errs = append(errs, errors.New("VERIFICATION_CRIMINAL_RECORD_MAX_DAYS, VERIFICATION_CERTIFICATE_MAX_MONTHS and VERIFICATION_EXPIRY_NOTICE_DAYS must be positive"))
	}
	if c.PendingUploadTTL <= 0 || c.ReminderDelay <= 0 || c.HousekeepingInterval <= 0 || c.RateWindow <= 0 || c.UploadRateLimit <= 0 || c.CheckRateLimit <= 0 {
		errs = append(errs, errors.New("VERIFICATION_PENDING_UPLOAD_TTL, VERIFICATION_REMINDER_DELAY, VERIFICATION_HOUSEKEEPING_INTERVAL and the verification rate limits must be positive"))
	}
	for name, mode := range map[string]string{"VERIFICATION_QUALITY_PROVIDER": c.QualityProvider, "VERIFICATION_OCR_PROVIDER": c.OCRProvider, "VERIFICATION_LIVENESS_PROVIDER": c.LivenessProvider} {
		if !verificationModes[mode] {
			errs = append(errs, fmt.Errorf("%s must be fake or manual, got %q", name, mode))
		}
		if mode == "fake" && env == enums.EnvProduction {
			errs = append(errs, fmt.Errorf("%s cannot be fake in production", name))
		}
	}
	return errs
}

type DispatchConfig struct {
	WatcherInterval     time.Duration `mapstructure:"DISPATCH_WATCHER_INTERVAL"`
	WatcherBatch        int           `mapstructure:"DISPATCH_WATCHER_BATCH"`
	ChannelTimeout      time.Duration `mapstructure:"DISPATCH_CHANNEL_TIMEOUT"`
	FareEstimateTTL     time.Duration `mapstructure:"FARE_ESTIMATE_TTL"`
	NearbyMinCount      int           `mapstructure:"NEARBY_MIN_CELL_COUNT"`
	NearbyDriversMax    int           `mapstructure:"NEARBY_DRIVERS_MAX"`
	MaxAccuracyM        float64       `mapstructure:"LOCATION_MAX_ACCURACY_M"`
	MaxSpeedKmh         float64       `mapstructure:"LOCATION_MAX_SPEED_KMH"`
	HistoryRetention    time.Duration `mapstructure:"LOCATION_HISTORY_RETENTION"`
	OfferDeepLink       string        `mapstructure:"OFFER_DEEP_LINK_BASE"`
	OperatorPhone       string        `mapstructure:"OPERATOR_ALERT_PHONE"`
	ShareBaseURL        string        `mapstructure:"SHARE_BASE_URL"`
	IdempotencyTTL      time.Duration `mapstructure:"IDEMPOTENCY_TTL"`
	NotificationWorkers int           `mapstructure:"NOTIFICATION_WORKERS"`

	DemandAreaResolution   int           `mapstructure:"DEMAND_AREA_RESOLUTION"`
	DemandQuoteWindow      time.Duration `mapstructure:"DEMAND_QUOTE_WINDOW"`
	DemandFactorTTL        time.Duration `mapstructure:"DEMAND_FACTOR_TTL"`
	DemandHistoryRetention time.Duration `mapstructure:"DEMAND_HISTORY_RETENTION"`
	RouteReviewAbovePct    int           `mapstructure:"ROUTE_REVIEW_ABOVE_PERCENT"`

	// Cadence of the provider app: every interval or every distance, per phase (ADR 0023)
	CadenceIdleInterval      time.Duration `mapstructure:"CADENCE_IDLE_INTERVAL"`
	CadenceIdleDistanceM     int           `mapstructure:"CADENCE_IDLE_DISTANCE_M"`
	CadenceApproachInterval  time.Duration `mapstructure:"CADENCE_APPROACH_INTERVAL"`
	CadenceApproachDistanceM int           `mapstructure:"CADENCE_APPROACH_DISTANCE_M"`
	CadenceTripInterval      time.Duration `mapstructure:"CADENCE_TRIP_INTERVAL"`
	CadenceTripDistanceM     int           `mapstructure:"CADENCE_TRIP_DISTANCE_M"`
	CadenceSlowdownFactor    int           `mapstructure:"CADENCE_SLOWDOWN_FACTOR"`
}

type RealtimeConfig struct {
	Heartbeat        time.Duration `mapstructure:"SSE_HEARTBEAT_INTERVAL"`
	PollInterval     time.Duration `mapstructure:"SSE_POLL_INTERVAL"`
	DeadAfter        time.Duration `mapstructure:"SSE_DEAD_AFTER"`
	MailboxEntries   int64         `mapstructure:"SSE_MAILBOX_MAX_ENTRIES"`
	MailboxTTL       time.Duration `mapstructure:"SSE_MAILBOX_TTL"`
	ReconnectSpread  time.Duration `mapstructure:"SSE_RECONNECT_SPREAD"`
	ProjectorWorkers int           `mapstructure:"REALTIME_PROJECTOR_WORKERS"`
}

type GoogleMapsConfig struct {
	APIKey          string        `mapstructure:"GOOGLE_MAPS_API_KEY"`
	BaseURL         string        `mapstructure:"GOOGLE_MAPS_BASE_URL"`
	Timeout         time.Duration `mapstructure:"GOOGLE_MAPS_TIMEOUT"`
	BreakerFailures int           `mapstructure:"GOOGLE_MAPS_BREAKER_FAILURES"`
	BreakerCooldown time.Duration `mapstructure:"GOOGLE_MAPS_BREAKER_COOLDOWN"`
	RoadFactor      float64       `mapstructure:"ROUTING_ROAD_FACTOR"`
	AverageSpeedKmh float64       `mapstructure:"ROUTING_FALLBACK_SPEED_KMH"`
}

func (c GoogleMapsConfig) Enabled() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

type FCMConfig struct {
	ProjectID       string        `mapstructure:"FCM_PROJECT_ID"`
	CredentialsJSON string        `mapstructure:"FCM_CREDENTIALS_JSON"`
	CredentialsFile string        `mapstructure:"FCM_CREDENTIALS_FILE"`
	BaseURL         string        `mapstructure:"FCM_BASE_URL"`
	Timeout         time.Duration `mapstructure:"FCM_TIMEOUT"`
}

func (c FCMConfig) Enabled() bool {
	return strings.TrimSpace(c.ProjectID) != "" && strings.TrimSpace(c.CredentialsJSON) != ""
}

func (c *FCMConfig) readCredentialsFile() error {
	path := strings.TrimSpace(c.CredentialsFile)
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path) // #nosec G304 -- the path comes from the operator's environment
	if err != nil {
		return fmt.Errorf("read FCM_CREDENTIALS_FILE: %w", err)
	}
	c.CredentialsJSON = string(content)
	return nil
}

// APNSConfig is the token-based (.p8) key the Live Activity of a ride is
// updated with. Without it those pushes are written to the log.
type APNSConfig struct {
	KeyID        string        `mapstructure:"APNS_KEY_ID"`
	TeamID       string        `mapstructure:"APNS_TEAM_ID"`
	KeyBase64    string        `mapstructure:"APNS_PRIVATE_KEY_BASE64"`
	BundleID     string        `mapstructure:"APNS_BUNDLE_ID"`
	BaseURL      string        `mapstructure:"APNS_BASE_URL"`
	Timeout      time.Duration `mapstructure:"APNS_TIMEOUT"`
	Interval     time.Duration `mapstructure:"RIDE_LIVE_INTERVAL"`
	StaleAfter   time.Duration `mapstructure:"RIDE_LIVE_STALE_AFTER"`
	DismissAfter time.Duration `mapstructure:"RIDE_LIVE_DISMISS_AFTER"`
}

func (c APNSConfig) Enabled() bool {
	return strings.TrimSpace(c.KeyID) != "" && strings.TrimSpace(c.TeamID) != "" && strings.TrimSpace(c.KeyBase64) != "" && strings.TrimSpace(c.BundleID) != ""
}

// KeyPEM is the .p8 file, kept base64 encoded (base64 -w0 AuthKey.p8) so it fits on one line.
func (c APNSConfig) KeyPEM() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.KeyBase64))
	if err != nil {
		return nil, fmt.Errorf("APNS_PRIVATE_KEY_BASE64 is not base64: %w", err)
	}
	return key, nil
}

func (c APNSConfig) validate() []error {
	var errs []error
	if c.Timeout <= 0 || strings.TrimSpace(c.BaseURL) == "" {
		errs = append(errs, errors.New("APNS_TIMEOUT must be positive and APNS_BASE_URL cannot be empty"))
	}
	if c.Interval < 0 || c.StaleAfter < 0 || c.DismissAfter < 0 {
		errs = append(errs, errors.New("RIDE_LIVE_INTERVAL, RIDE_LIVE_STALE_AFTER and RIDE_LIVE_DISMISS_AFTER cannot be negative"))
	}
	if c.Enabled() {
		if _, err := c.KeyPEM(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

type AppConfig struct {
	Name string            `mapstructure:"APP_NAME"`
	Env  enums.Environment `mapstructure:"APP_ENV"`
}

type HTTPConfig struct {
	Port               int           `mapstructure:"HTTP_PORT"`
	ReadTimeout        time.Duration `mapstructure:"HTTP_READ_TIMEOUT"`
	WriteTimeout       time.Duration `mapstructure:"HTTP_WRITE_TIMEOUT"`
	IdleTimeout        time.Duration `mapstructure:"HTTP_IDLE_TIMEOUT"`
	ShutdownTimeout    time.Duration `mapstructure:"HTTP_SHUTDOWN_TIMEOUT"`
	CORSAllowedOrigins []string      `mapstructure:"CORS_ALLOWED_ORIGINS"`
	TrustedProxies     []string      `mapstructure:"TRUSTED_PROXIES"`
	RateLimitRequests  int           `mapstructure:"RATE_LIMIT_REQUESTS"`
	RateLimitWindow    time.Duration `mapstructure:"RATE_LIMIT_WINDOW"`
	AuthRateLimit      int           `mapstructure:"AUTH_RATE_LIMIT_REQUESTS"`
	AuthRateWindow     time.Duration `mapstructure:"AUTH_RATE_LIMIT_WINDOW"`
}

type LogConfig struct {
	Level  string `mapstructure:"LOG_LEVEL"`
	Output string `mapstructure:"LOG_OUTPUT"`
}

// TelemetryConfig controls traces and where /metrics is served.
type TelemetryConfig struct {
	// OTLPEndpoint is the OTLP gRPC collector (http://alloy:4317). Empty
	// turns tracing off.
	OTLPEndpoint string `mapstructure:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	// TraceSampleRatio is the share of new traces exported, in (0, 1].
	TraceSampleRatio float64 `mapstructure:"OTEL_TRACES_SAMPLE_RATIO"`
	// MetricsPort serves /metrics on its own internal listener. Zero keeps
	// it on the public HTTP port.
	MetricsPort int `mapstructure:"METRICS_PORT"`
}

func (c TelemetryConfig) TracingEnabled() bool {
	return strings.TrimSpace(c.OTLPEndpoint) != ""
}

type DatabaseConfig struct {
	URL             string        `mapstructure:"DATABASE_URL"`
	MaxConns        int32         `mapstructure:"DB_MAX_CONNS"`
	MinConns        int32         `mapstructure:"DB_MIN_CONNS"`
	MaxConnLifetime time.Duration `mapstructure:"DB_MAX_CONN_LIFETIME"`
	MaxConnIdleTime time.Duration `mapstructure:"DB_MAX_CONN_IDLE_TIME"`
	MigrateOnStart  bool          `mapstructure:"DB_MIGRATE_ON_START"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"REDIS_ADDR"`
	Password string `mapstructure:"REDIS_PASSWORD"`
	DB       int    `mapstructure:"REDIS_DB"`
}

type RabbitMQConfig struct {
	URL        string        `mapstructure:"RABBITMQ_URL"`
	Exchange   string        `mapstructure:"RABBITMQ_EXCHANGE"`
	Prefetch   int           `mapstructure:"RABBITMQ_PREFETCH"`
	MaxRetries int           `mapstructure:"RABBITMQ_MAX_RETRIES"`
	RetryDelay time.Duration `mapstructure:"RABBITMQ_RETRY_DELAY"`
	DedupTTL   time.Duration `mapstructure:"EVENT_DEDUP_TTL"`
}

type OutboxConfig struct {
	PollInterval time.Duration `mapstructure:"OUTBOX_POLL_INTERVAL"`
	BatchSize    int32         `mapstructure:"OUTBOX_BATCH_SIZE"`
}

type JWTConfig struct {
	Secret     string        `mapstructure:"JWT_SECRET"`
	Issuer     string        `mapstructure:"JWT_ISSUER"`
	AccessTTL  time.Duration `mapstructure:"JWT_ACCESS_TTL"`
	RefreshTTL time.Duration `mapstructure:"JWT_REFRESH_TTL"`
}

type OTPConfig struct {
	TTL            time.Duration `mapstructure:"OTP_TTL"`
	MaxAttempts    int           `mapstructure:"OTP_MAX_ATTEMPTS"`
	ResendCooldown time.Duration `mapstructure:"OTP_RESEND_COOLDOWN"`
}

type SMSConfig struct {
	APIKey   string        `mapstructure:"CASTBRICK_API_KEY"`
	BaseURL  string        `mapstructure:"CASTBRICK_BASE_URL"`
	SenderID string        `mapstructure:"CASTBRICK_SENDER_ID"`
	Timeout  time.Duration `mapstructure:"CASTBRICK_TIMEOUT"`
}

type EmailConfig struct {
	APIKey  string        `mapstructure:"RESEND_API_KEY"`
	BaseURL string        `mapstructure:"RESEND_BASE_URL"`
	Timeout time.Duration `mapstructure:"RESEND_TIMEOUT"`
	From    string        `mapstructure:"EMAIL_FROM"`
	ReplyTo string        `mapstructure:"EMAIL_REPLY_TO"`
}

func (c EmailConfig) Enabled() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

type AccessConfig struct {
	PermissionCacheTTL   time.Duration `mapstructure:"PERMISSION_CACHE_TTL"`
	InvitationBackoffice string        `mapstructure:"INVITATION_URL_BACKOFFICE"`
	InvitationPartner    string        `mapstructure:"INVITATION_URL_PARTNER_PANEL"`
}

func (c SMSConfig) Enabled() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

var defaults = map[string]any{
	"APP_NAME":                              "atlas",
	"APP_ENV":                               "development",
	"HTTP_PORT":                             8700,
	"HTTP_READ_TIMEOUT":                     "15s",
	"HTTP_WRITE_TIMEOUT":                    "15s",
	"HTTP_IDLE_TIMEOUT":                     "60s",
	"HTTP_SHUTDOWN_TIMEOUT":                 "20s",
	"CORS_ALLOWED_ORIGINS":                  "*",
	"TRUSTED_PROXIES":                       "",
	"RATE_LIMIT_REQUESTS":                   100,
	"RATE_LIMIT_WINDOW":                     "1m",
	"AUTH_RATE_LIMIT_REQUESTS":              10,
	"AUTH_RATE_LIMIT_WINDOW":                "1m",
	"LOG_LEVEL":                             "info",
	"OTEL_EXPORTER_OTLP_ENDPOINT":           "",
	"OTEL_TRACES_SAMPLE_RATIO":              1.0,
	"METRICS_PORT":                          0,
	"LOG_OUTPUT":                            "stdout",
	"DB_MAX_CONNS":                          20,
	"DB_MIN_CONNS":                          2,
	"DB_MAX_CONN_LIFETIME":                  "1h",
	"DB_MAX_CONN_IDLE_TIME":                 "15m",
	"DB_MIGRATE_ON_START":                   true,
	"REDIS_PASSWORD":                        "",
	"REDIS_DB":                              0,
	"RABBITMQ_EXCHANGE":                     "agora.events",
	"RABBITMQ_PREFETCH":                     10,
	"RABBITMQ_MAX_RETRIES":                  5,
	"RABBITMQ_RETRY_DELAY":                  "10s",
	"EVENT_DEDUP_TTL":                       "24h",
	"OUTBOX_POLL_INTERVAL":                  "1s",
	"OUTBOX_BATCH_SIZE":                     100,
	"JWT_ISSUER":                            "atlas",
	"JWT_ACCESS_TTL":                        "15m",
	"JWT_REFRESH_TTL":                       "720h",
	"OTP_TTL":                               "5m",
	"OTP_MAX_ATTEMPTS":                      5,
	"OTP_RESEND_COOLDOWN":                   "60s",
	"CASTBRICK_API_KEY":                     "",
	"CASTBRICK_BASE_URL":                    "https://api.castbrick.co/v1",
	"CASTBRICK_SENDER_ID":                   "",
	"CASTBRICK_TIMEOUT":                     "10s",
	"RESEND_API_KEY":                        "",
	"RESEND_BASE_URL":                       "https://api.resend.com",
	"RESEND_TIMEOUT":                        "10s",
	"EMAIL_FROM":                            "",
	"EMAIL_REPLY_TO":                        "",
	"PERMISSION_CACHE_TTL":                  "5m",
	"INVITATION_URL_BACKOFFICE":             "http://localhost:3001/convite",
	"INVITATION_URL_PARTNER_PANEL":          "http://localhost:3002/convite",
	"DISPATCH_WATCHER_INTERVAL":             "1s",
	"DISPATCH_WATCHER_BATCH":                200,
	"DISPATCH_CHANNEL_TIMEOUT":              "5s",
	"FARE_ESTIMATE_TTL":                     "5m",
	"NEARBY_MIN_CELL_COUNT":                 3,
	"LOCATION_MAX_ACCURACY_M":               100,
	"LOCATION_MAX_SPEED_KMH":                200,
	"LOCATION_HISTORY_RETENTION":            "720h",
	"OFFER_DEEP_LINK_BASE":                  "agoraprovider://app",
	"NEARBY_DRIVERS_MAX":                    20,
	"OPERATOR_ALERT_PHONE":                  "",
	"IDEMPOTENCY_TTL":                       "24h",
	"NOTIFICATION_WORKERS":                  8,
	"DEMAND_AREA_RESOLUTION":                7,
	"DEMAND_QUOTE_WINDOW":                   "3m",
	"DEMAND_FACTOR_TTL":                     "3m",
	"DEMAND_HISTORY_RETENTION":              "720h",
	"ROUTE_REVIEW_ABOVE_PERCENT":            30,
	"CADENCE_IDLE_INTERVAL":                 "30s",
	"CADENCE_IDLE_DISTANCE_M":               150,
	"CADENCE_APPROACH_INTERVAL":             "7s",
	"CADENCE_APPROACH_DISTANCE_M":           30,
	"CADENCE_TRIP_INTERVAL":                 "10s",
	"CADENCE_TRIP_DISTANCE_M":               50,
	"CADENCE_SLOWDOWN_FACTOR":               2,
	"HEATMAP_INTERVAL":                      "5m",
	"HEATMAP_WINDOW":                        "15m",
	"HEATMAP_LOOKBACK_WINDOWS":              2,
	"HEATMAP_ROLLUP_RESOLUTIONS":            "8,7,6",
	"HEATMAP_SELF_WEIGHT":                   0.6,
	"HEATMAP_NEIGHBOR_WEIGHT":               0.4,
	"HEATMAP_ZOOM_RESOLUTIONS":              "0:6,11:7,13:8,15:9",
	"HEATMAP_MAX_CELLS":                     20000,
	"HEATMAP_MAX_PERIOD":                    "168h",
	"HEATMAP_CACHE_TTL":                     "60s",
	"HEATMAP_RETENTION":                     "2160h",
	"REALTIME_PROJECTOR_WORKERS":            16,
	"SSE_HEARTBEAT_INTERVAL":                "15s",
	"SSE_POLL_INTERVAL":                     "30s",
	"SSE_DEAD_AFTER":                        "38s",
	"SSE_MAILBOX_MAX_ENTRIES":               200,
	"SSE_MAILBOX_TTL":                       "24h",
	"SSE_RECONNECT_SPREAD":                  "30s",
	"GOOGLE_MAPS_API_KEY":                   "",
	"GOOGLE_MAPS_BASE_URL":                  "https://routes.googleapis.com",
	"GOOGLE_MAPS_TIMEOUT":                   "2s",
	"GOOGLE_MAPS_BREAKER_FAILURES":          5,
	"GOOGLE_MAPS_BREAKER_COOLDOWN":          "30s",
	"ROUTING_ROAD_FACTOR":                   1.4,
	"ROUTING_FALLBACK_SPEED_KMH":            25,
	"FCM_PROJECT_ID":                        "",
	"FCM_CREDENTIALS_JSON":                  "",
	"FCM_CREDENTIALS_FILE":                  "",
	"FCM_BASE_URL":                          "https://fcm.googleapis.com",
	"FCM_TIMEOUT":                           "5s",
	"APNS_KEY_ID":                           "",
	"APNS_TEAM_ID":                          "",
	"APNS_PRIVATE_KEY_BASE64":               "",
	"APNS_BUNDLE_ID":                        "",
	"APNS_BASE_URL":                         "https://api.push.apple.com",
	"APNS_TIMEOUT":                          "5s",
	"RIDE_LIVE_INTERVAL":                    "5s",
	"RIDE_LIVE_STALE_AFTER":                 "2m",
	"RIDE_LIVE_DISMISS_AFTER":               "5m",
	"APPYPAY_CLIENT_ID":                     "",
	"APPYPAY_CLIENT_SECRET":                 "",
	"APPYPAY_BASE_URL":                      "",
	"APPYPAY_TOKEN_URL":                     "",
	"APPYPAY_RESOURCE":                      "",
	"APPYPAY_GPO_METHOD":                    "",
	"APPYPAY_REF_METHOD":                    "",
	"APPYPAY_TIMEOUT":                       "30s",
	"PAYMENT_GPO_WINDOW":                    "5m",
	"PAYMENT_REFERENCE_TTL":                 "72h",
	"PAYMENT_RECONCILE_INTERVAL":            "15s",
	"PAYMENT_RECONCILE_BATCH":               50,
	"PAYMENT_GPO_CHECK_INTERVAL":            "15s",
	"PAYMENT_REFERENCE_CHECK_INTERVAL":      "10m",
	"LEDGER_CURRENCY":                       "AOA",
	"CALLS_BASE_URL":                        "",
	"CALLS_API_KEY":                         "",
	"CALLS_TIMEOUT":                         "5s",
	"SHARE_BASE_URL":                        "https://agora.ao/v",
	"R2_ACCOUNT_ID":                         "",
	"R2_ACCESS_KEY_ID":                      "",
	"R2_SECRET_ACCESS_KEY":                  "",
	"R2_BUCKET_NAME":                        "",
	"R2_ENDPOINT":                           "",
	"R2_PRESIGN_TTL":                        "15m",
	"VERIFICATION_MAX_FILE_BYTES":           10485760,
	"VERIFICATION_READ_URL_TTL":             "5m",
	"VERIFICATION_MIN_PROVIDER_AGE":         18,
	"VERIFICATION_MAX_ATTEMPTS":             3,
	"VERIFICATION_CRIMINAL_RECORD_MAX_DAYS": 90,
	"VERIFICATION_CERTIFICATE_MAX_MONTHS":   6,
	"VERIFICATION_EXPIRY_NOTICE_DAYS":       30,
	"VERIFICATION_PENDING_UPLOAD_TTL":       "24h",
	"VERIFICATION_REMINDER_DELAY":           "24h",
	"VERIFICATION_HOUSEKEEPING_INTERVAL":    "1h",
	"VERIFICATION_UPLOAD_RATE_LIMIT":        60,
	"VERIFICATION_CHECK_RATE_LIMIT":         15,
	"VERIFICATION_RATE_WINDOW":              "1m",
	"VERIFICATION_QUALITY_PROVIDER":         "manual",
	"VERIFICATION_OCR_PROVIDER":             "manual",
	"VERIFICATION_LIVENESS_PROVIDER":        "manual",
	"VERIFICATION_FAKE_QUALITY_REASONS":     "",
	"VERIFICATION_FAKE_OCR_FAIL":            false,
	"VERIFICATION_FAKE_LIVENESS_FAIL":       false,
	"VERIFICATION_WORKERS":                  4,
}

const cloudflareIdleLimit = 100 * time.Second

// minCadenceInterval keeps the provider app from reporting more than every 2 s on the way and during the trip (ADR 0023)
const minCadenceInterval = 2 * time.Second

const (
	minDemandAreaResolution = 5
	maxDemandAreaResolution = 8
	maxNearbyDrivers        = 100
	percentToBps            = 100
)

func (d DispatchConfig) RouteReviewAboveBps() int {
	return d.RouteReviewAbovePct * percentToBps
}

var required = []string{"DATABASE_URL", "REDIS_ADDR", "RABBITMQ_URL", "JWT_SECRET"}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}
	for _, key := range required {
		_ = v.BindEnv(key)
	}
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	cfg.HTTP.CORSAllowedOrigins = compact(cfg.HTTP.CORSAllowedOrigins)
	cfg.HTTP.TrustedProxies = compact(cfg.HTTP.TrustedProxies)
	cfg.Verify.FakeQualityReasons = compact(cfg.Verify.FakeQualityReasons)
	if err := cfg.FCM.readCredentialsFile(); err != nil {
		return nil, err
	}

	var errs []error
	for _, key := range required {
		if strings.TrimSpace(v.GetString(key)) == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
	}
	if err := errors.Join(append(errs, cfg.validate())...); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	var errs []error

	if !c.App.Env.IsValid() {
		errs = append(errs, fmt.Errorf("APP_ENV must be one of %v, got %q", enums.Environments(), c.App.Env))
	}
	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		errs = append(errs, fmt.Errorf("HTTP_PORT out of range: %d", c.HTTP.Port))
	}
	if c.Telemetry.TraceSampleRatio <= 0 || c.Telemetry.TraceSampleRatio > 1 {
		errs = append(errs, fmt.Errorf("OTEL_TRACES_SAMPLE_RATIO must be in (0, 1], got %v", c.Telemetry.TraceSampleRatio))
	}
	if c.Telemetry.MetricsPort < 0 || c.Telemetry.MetricsPort > 65535 || (c.Telemetry.MetricsPort != 0 && c.Telemetry.MetricsPort == c.HTTP.Port) {
		errs = append(errs, fmt.Errorf("METRICS_PORT must be 0 or a free port other than HTTP_PORT, got %d", c.Telemetry.MetricsPort))
	}
	if c.HTTP.RateLimitRequests <= 0 || c.HTTP.RateLimitWindow <= 0 {
		errs = append(errs, errors.New("RATE_LIMIT_REQUESTS and RATE_LIMIT_WINDOW must be positive"))
	}
	if c.Database.MinConns > c.Database.MaxConns {
		errs = append(errs, errors.New("DB_MIN_CONNS cannot exceed DB_MAX_CONNS"))
	}
	if c.RabbitMQ.MaxRetries < 0 || c.RabbitMQ.Prefetch <= 0 {
		errs = append(errs, errors.New("RABBITMQ_MAX_RETRIES must be >= 0 and RABBITMQ_PREFETCH > 0"))
	}
	if c.Outbox.BatchSize <= 0 || c.Outbox.PollInterval <= 0 {
		errs = append(errs, errors.New("OUTBOX_BATCH_SIZE and OUTBOX_POLL_INTERVAL must be positive"))
	}
	if c.HTTP.AuthRateLimit <= 0 || c.HTTP.AuthRateWindow <= 0 {
		errs = append(errs, errors.New("AUTH_RATE_LIMIT_REQUESTS and AUTH_RATE_LIMIT_WINDOW must be positive"))
	}
	if c.JWT.RefreshTTL <= c.JWT.AccessTTL {
		errs = append(errs, errors.New("JWT_REFRESH_TTL must be longer than JWT_ACCESS_TTL"))
	}
	if c.OTP.TTL <= 0 || c.OTP.MaxAttempts <= 0 || c.OTP.ResendCooldown < 0 {
		errs = append(errs, errors.New("OTP_TTL and OTP_MAX_ATTEMPTS must be positive and OTP_RESEND_COOLDOWN >= 0"))
	}
	if c.SMS.Timeout <= 0 || strings.TrimSpace(c.SMS.BaseURL) == "" {
		errs = append(errs, errors.New("CASTBRICK_TIMEOUT must be positive and CASTBRICK_BASE_URL cannot be empty"))
	}
	if c.Email.Timeout <= 0 || strings.TrimSpace(c.Email.BaseURL) == "" {
		errs = append(errs, errors.New("RESEND_TIMEOUT must be positive and RESEND_BASE_URL cannot be empty"))
	}
	if c.Email.Enabled() && strings.TrimSpace(c.Email.From) == "" {
		errs = append(errs, errors.New("EMAIL_FROM is required when RESEND_API_KEY is set"))
	}
	if len(c.JWT.Secret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if c.Access.PermissionCacheTTL <= 0 {
		errs = append(errs, errors.New("PERMISSION_CACHE_TTL must be positive"))
	}
	if strings.TrimSpace(c.Access.InvitationBackoffice) == "" || strings.TrimSpace(c.Access.InvitationPartner) == "" {
		errs = append(errs, errors.New("INVITATION_URL_BACKOFFICE and INVITATION_URL_PARTNER_PANEL cannot be empty"))
	}
	errs = append(errs, c.validateDispatch()...)
	errs = append(errs, c.Heatmap.prepare()...)
	if c.AppyPay.Timeout <= 0 || c.AppyPay.GPOWindow <= 0 || c.AppyPay.ReferenceTTL <= 0 || c.AppyPay.ReconcileInterval <= 0 || c.AppyPay.ReconcileBatch <= 0 || c.AppyPay.GPOCheckInterval <= 0 || c.AppyPay.ReferenceCheckInterval <= 0 {
		errs = append(errs, errors.New("APPYPAY_TIMEOUT and the PAYMENT_* settings must be positive"))
	}
	if c.Calls.Timeout <= 0 {
		errs = append(errs, errors.New("CALLS_TIMEOUT must be positive"))
	}
	if strings.TrimSpace(c.Dispatch.ShareBaseURL) == "" {
		errs = append(errs, errors.New("SHARE_BASE_URL cannot be empty"))
	}
	if len(c.AppyPay.LedgerCurrency) != 3 {
		errs = append(errs, errors.New("LEDGER_CURRENCY must have 3 letters"))
	}
	if c.App.Env == enums.EnvProduction {
		if !c.AppyPay.Enabled() {
			errs = append(errs, errors.New("every APPYPAY_* credential is required in production"))
		}
		if !c.SMS.Enabled() {
			errs = append(errs, errors.New("CASTBRICK_API_KEY is required in production"))
		}
		if !c.Email.Enabled() {
			errs = append(errs, errors.New("RESEND_API_KEY is required in production"))
		}
		if !c.R2.Enabled() {
			errs = append(errs, errors.New("R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY and R2_BUCKET_NAME are required in production"))
		}
		for _, o := range c.HTTP.CORSAllowedOrigins {
			if o == "*" {
				errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS cannot be * in production"))
			}
		}
	}

	return errors.Join(errs...)
}

func (c *Config) validateDispatch() []error {
	var errs []error
	if c.Dispatch.WatcherInterval <= 0 || c.Dispatch.WatcherBatch <= 0 || c.Dispatch.ChannelTimeout <= 0 {
		errs = append(errs, errors.New("DISPATCH_WATCHER_INTERVAL, DISPATCH_WATCHER_BATCH and DISPATCH_CHANNEL_TIMEOUT must be positive"))
	}
	if c.Dispatch.FareEstimateTTL <= 0 || c.Dispatch.NearbyMinCount <= 0 || c.Dispatch.IdempotencyTTL <= 0 {
		errs = append(errs, errors.New("FARE_ESTIMATE_TTL, NEARBY_MIN_CELL_COUNT and IDEMPOTENCY_TTL must be positive"))
	}
	if c.Dispatch.NearbyDriversMax <= 0 || c.Dispatch.NearbyDriversMax > maxNearbyDrivers {
		errs = append(errs, fmt.Errorf("NEARBY_DRIVERS_MAX must be between 1 and %d", maxNearbyDrivers))
	}
	if c.Dispatch.MaxAccuracyM <= 0 || c.Dispatch.MaxSpeedKmh <= 0 {
		errs = append(errs, errors.New("LOCATION_MAX_ACCURACY_M and LOCATION_MAX_SPEED_KMH must be positive"))
	}
	if strings.TrimSpace(c.Dispatch.OfferDeepLink) == "" {
		errs = append(errs, errors.New("OFFER_DEEP_LINK_BASE cannot be empty"))
	}
	if c.Dispatch.DemandAreaResolution < minDemandAreaResolution || c.Dispatch.DemandAreaResolution > maxDemandAreaResolution {
		errs = append(errs, fmt.Errorf("DEMAND_AREA_RESOLUTION must be between %d and %d", minDemandAreaResolution, maxDemandAreaResolution))
	}
	if c.Dispatch.DemandQuoteWindow <= 0 || c.Dispatch.DemandFactorTTL <= 0 || c.Dispatch.DemandHistoryRetention < 0 {
		errs = append(errs, errors.New("DEMAND_QUOTE_WINDOW and DEMAND_FACTOR_TTL must be positive and DEMAND_HISTORY_RETENTION cannot be negative"))
	}
	if c.Dispatch.RouteReviewAbovePct < 0 {
		errs = append(errs, errors.New("ROUTE_REVIEW_ABOVE_PERCENT cannot be negative"))
	}
	if c.Dispatch.CadenceIdleInterval <= 0 || c.Dispatch.CadenceApproachInterval < minCadenceInterval || c.Dispatch.CadenceTripInterval < minCadenceInterval {
		errs = append(errs, fmt.Errorf("CADENCE_IDLE_INTERVAL must be positive, and CADENCE_APPROACH_INTERVAL and CADENCE_TRIP_INTERVAL at least %s", minCadenceInterval))
	}
	if c.Dispatch.CadenceIdleDistanceM <= 0 || c.Dispatch.CadenceApproachDistanceM <= 0 || c.Dispatch.CadenceTripDistanceM <= 0 || c.Dispatch.CadenceSlowdownFactor < 1 {
		errs = append(errs, errors.New("CADENCE_*_DISTANCE_M must be positive and CADENCE_SLOWDOWN_FACTOR at least 1"))
	}
	if c.Realtime.Heartbeat <= 0 || c.Realtime.Heartbeat >= cloudflareIdleLimit {
		errs = append(errs, errors.New("SSE_HEARTBEAT_INTERVAL must be positive and below the 100s Cloudflare idle limit"))
	}
	if c.Realtime.PollInterval <= 0 || c.Realtime.DeadAfter <= c.Realtime.Heartbeat || c.Realtime.ReconnectSpread < 0 {
		errs = append(errs, errors.New("SSE_POLL_INTERVAL must be positive, SSE_DEAD_AFTER above the heartbeat and SSE_RECONNECT_SPREAD >= 0"))
	}
	if c.Realtime.MailboxEntries <= 0 || c.Realtime.MailboxTTL <= 0 {
		errs = append(errs, errors.New("SSE_MAILBOX_MAX_ENTRIES and SSE_MAILBOX_TTL must be positive"))
	}
	if c.Realtime.ProjectorWorkers <= 0 || c.Dispatch.NotificationWorkers <= 0 {
		errs = append(errs, errors.New("REALTIME_PROJECTOR_WORKERS and NOTIFICATION_WORKERS must be positive"))
	}
	if c.GoogleMaps.Timeout <= 0 || c.GoogleMaps.BreakerFailures <= 0 || c.GoogleMaps.BreakerCooldown <= 0 || strings.TrimSpace(c.GoogleMaps.BaseURL) == "" {
		errs = append(errs, errors.New("GOOGLE_MAPS_TIMEOUT, GOOGLE_MAPS_BREAKER_FAILURES and GOOGLE_MAPS_BREAKER_COOLDOWN must be positive and GOOGLE_MAPS_BASE_URL cannot be empty"))
	}
	if c.GoogleMaps.RoadFactor < 1 || c.GoogleMaps.AverageSpeedKmh <= 0 {
		errs = append(errs, errors.New("ROUTING_ROAD_FACTOR must be at least 1 and ROUTING_FALLBACK_SPEED_KMH positive"))
	}
	if c.R2.PresignTTL <= 0 {
		errs = append(errs, errors.New("R2_PRESIGN_TTL must be positive"))
	}
	errs = append(errs, c.Verify.validate(c.App.Env)...)
	if c.FCM.Timeout <= 0 || strings.TrimSpace(c.FCM.BaseURL) == "" {
		errs = append(errs, errors.New("FCM_TIMEOUT must be positive and FCM_BASE_URL cannot be empty"))
	}
	errs = append(errs, c.APNS.validate()...)
	return errs
}

// compact trims entries and drops empty ones, so "" or "a, ,b" parse cleanly.
func compact(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// IsProduction reports whether the application runs in production.
func (c *Config) IsProduction() bool {
	return c.App.Env == enums.EnvProduction
}

func (c *Config) IsDevelopment() bool {
	return c.App.Env == enums.EnvDevelopment
}
