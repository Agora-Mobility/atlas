// Package config loads and validates the application configuration from
// environment variables with Viper. Every setting comes from the environment;
// a .env file is only a convenience for local development.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Config struct {
	App      AppConfig      `mapstructure:",squash"`
	HTTP     HTTPConfig     `mapstructure:",squash"`
	Log      LogConfig      `mapstructure:",squash"`
	Database DatabaseConfig `mapstructure:",squash"`
	Redis    RedisConfig    `mapstructure:",squash"`
	RabbitMQ RabbitMQConfig `mapstructure:",squash"`
	Outbox   OutboxConfig   `mapstructure:",squash"`
	JWT      JWTConfig      `mapstructure:",squash"`
	OTP      OTPConfig      `mapstructure:",squash"`
	SMS      SMSConfig      `mapstructure:",squash"`
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

type DatabaseConfig struct {
	URL             string        `mapstructure:"DATABASE_URL"`
	MaxConns        int32         `mapstructure:"DB_MAX_CONNS"`
	MinConns        int32         `mapstructure:"DB_MIN_CONNS"`
	MaxConnLifetime time.Duration `mapstructure:"DB_MAX_CONN_LIFETIME"`
	MaxConnIdleTime time.Duration `mapstructure:"DB_MAX_CONN_IDLE_TIME"`
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

func (c SMSConfig) Enabled() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

var defaults = map[string]any{
	"APP_NAME":                 "atlas",
	"APP_ENV":                  "development",
	"HTTP_PORT":                8080,
	"HTTP_READ_TIMEOUT":        "15s",
	"HTTP_WRITE_TIMEOUT":       "15s",
	"HTTP_IDLE_TIMEOUT":        "60s",
	"HTTP_SHUTDOWN_TIMEOUT":    "20s",
	"CORS_ALLOWED_ORIGINS":     "*",
	"TRUSTED_PROXIES":          "",
	"RATE_LIMIT_REQUESTS":      100,
	"RATE_LIMIT_WINDOW":        "1m",
	"AUTH_RATE_LIMIT_REQUESTS": 10,
	"AUTH_RATE_LIMIT_WINDOW":   "1m",
	"LOG_LEVEL":                "info",
	"LOG_OUTPUT":               "stdout",
	"DB_MAX_CONNS":             20,
	"DB_MIN_CONNS":             2,
	"DB_MAX_CONN_LIFETIME":     "1h",
	"DB_MAX_CONN_IDLE_TIME":    "15m",
	"REDIS_PASSWORD":           "",
	"REDIS_DB":                 0,
	"RABBITMQ_EXCHANGE":        "agora.events",
	"RABBITMQ_PREFETCH":        10,
	"RABBITMQ_MAX_RETRIES":     5,
	"RABBITMQ_RETRY_DELAY":     "10s",
	"EVENT_DEDUP_TTL":          "24h",
	"OUTBOX_POLL_INTERVAL":     "1s",
	"OUTBOX_BATCH_SIZE":        100,
	"JWT_ISSUER":               "atlas",
	"JWT_ACCESS_TTL":           "15m",
	"JWT_REFRESH_TTL":          "720h",
	"OTP_TTL":                  "5m",
	"OTP_MAX_ATTEMPTS":         5,
	"OTP_RESEND_COOLDOWN":      "60s",
	"CASTBRICK_API_KEY":        "",
	"CASTBRICK_BASE_URL":       "https://api.castbrick.co/v1",
	"CASTBRICK_SENDER_ID":      "",
	"CASTBRICK_TIMEOUT":        "10s",
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
	if len(c.JWT.Secret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if c.App.Env == enums.EnvProduction {
		if !c.SMS.Enabled() {
			errs = append(errs, errors.New("CASTBRICK_API_KEY is required in production"))
		}
		for _, o := range c.HTTP.CORSAllowedOrigins {
			if o == "*" {
				errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS cannot be * in production"))
			}
		}
	}

	return errors.Join(errs...)
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
