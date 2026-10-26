// Package config loads and validates the application configuration from
// environment variables. Every setting comes from the environment; a .env file
// is only a convenience for local development.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"

	"github.com/Agora-Mobility/atlas/shared/enums"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Log      LogConfig
	Database DatabaseConfig
	Redis    RedisConfig
	RabbitMQ RabbitMQConfig
	Outbox   OutboxConfig
	JWT      JWTConfig
}

type AppConfig struct {
	Name string            `env:"APP_NAME" envDefault:"atlas"`
	Env  enums.Environment `env:"APP_ENV" envDefault:"development"`
}

type HTTPConfig struct {
	Port               int           `env:"HTTP_PORT" envDefault:"8080"`
	ReadTimeout        time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout       time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout        time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout    time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"20s"`
	CORSAllowedOrigins []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"*"`
	RateLimitRequests  int           `env:"RATE_LIMIT_REQUESTS" envDefault:"100"`
	RateLimitWindow    time.Duration `env:"RATE_LIMIT_WINDOW" envDefault:"1m"`
}

type LogConfig struct {
	Level string `env:"LOG_LEVEL" envDefault:"info"`
}

type DatabaseConfig struct {
	URL             string        `env:"DATABASE_URL,required,notEmpty"`
	MaxConns        int32         `env:"DB_MAX_CONNS" envDefault:"20"`
	MinConns        int32         `env:"DB_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime time.Duration `env:"DB_MAX_CONN_LIFETIME" envDefault:"1h"`
	MaxConnIdleTime time.Duration `env:"DB_MAX_CONN_IDLE_TIME" envDefault:"15m"`
}

type RedisConfig struct {
	Addr     string `env:"REDIS_ADDR,required,notEmpty"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
}

type RabbitMQConfig struct {
	URL        string        `env:"RABBITMQ_URL,required,notEmpty"`
	Exchange   string        `env:"RABBITMQ_EXCHANGE" envDefault:"agora.events"`
	Prefetch   int           `env:"RABBITMQ_PREFETCH" envDefault:"10"`
	MaxRetries int           `env:"RABBITMQ_MAX_RETRIES" envDefault:"5"`
	RetryDelay time.Duration `env:"RABBITMQ_RETRY_DELAY" envDefault:"10s"`
	DedupTTL   time.Duration `env:"EVENT_DEDUP_TTL" envDefault:"24h"`
}

type OutboxConfig struct {
	PollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	BatchSize    int32         `env:"OUTBOX_BATCH_SIZE" envDefault:"100"`
}

type JWTConfig struct {
	Secret    string        `env:"JWT_SECRET,required,notEmpty"`
	Issuer    string        `env:"JWT_ISSUER" envDefault:"atlas"`
	AccessTTL time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
}

// LoadDotEnv loads a .env file when present. A missing file is not an error:
// in containers the variables come from the environment.
func LoadDotEnv() {
	_ = godotenv.Load()
}

// Load parses the environment into a Config and validates it. It is meant to
// be called once at startup so the process fails fast on bad configuration.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.validate(); err != nil {
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
	if len(c.JWT.Secret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if c.App.Env == enums.EnvProduction {
		for _, o := range c.HTTP.CORSAllowedOrigins {
			if strings.TrimSpace(o) == "*" {
				errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS cannot be * in production"))
			}
		}
	}

	return errors.Join(errs...)
}

// IsProduction reports whether the application runs in production.
func (c *Config) IsProduction() bool {
	return c.App.Env == enums.EnvProduction
}
