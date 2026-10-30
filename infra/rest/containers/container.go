package containers

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/cache"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/redis"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/notification/sms"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/queue"
	"github.com/Agora-Mobility/atlas/infra/repository"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/infra/security/revocation"
	"github.com/Agora-Mobility/atlas/infra/sse"
)

type Container struct {
	Config  *config.Config
	Version string

	Pool  *pgxpool.Pool
	Redis *goredis.Client
	Queue *queue.Manager

	Cache              cache.Cache
	TxManager          *postgres.TxManager
	Outbox             *outbox.Repository
	Publisher          *queue.Publisher
	Deduplicator       *queue.Deduplicator
	JWT                *authjwt.Service
	SessionRevocations services.ISessionRevocationStore
	SSE                *sse.Hub
	Authenticator      *middleware.Authenticator
	ClientAuthorizer   *middleware.ClientAuthorizer
	SMS                services.ISMSSender

	Health        *HealthModule
	Auth          *AuthModule
	Notifications *NotificationsModule
}

type Connections struct {
	Pool  *pgxpool.Pool
	Redis *goredis.Client
	Queue *queue.Manager
}

func Connect(ctx context.Context, cfg *config.Config) (*Connections, error) {
	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	rdb, err := redis.NewClient(ctx, cfg.Redis)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	mgr, err := queue.NewManager(ctx, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, err
	}
	return &Connections{Pool: pool, Redis: rdb, Queue: mgr}, nil
}

func New(cfg *config.Config, version string, conn *Connections) (*Container, error) {
	jwt := authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.AccessTTL)
	sessionRevocations := revocation.NewRedisStore(conn.Redis, cfg.App.Name+":", cfg.JWT.AccessTTL)
	c := &Container{
		Config:             cfg,
		Version:            version,
		Pool:               conn.Pool,
		Redis:              conn.Redis,
		Queue:              conn.Queue,
		Cache:              cache.NewRedisCache(conn.Redis, cfg.App.Name+":cache:"),
		TxManager:          postgres.NewTxManager(conn.Pool),
		Outbox:             outbox.NewRepository(conn.Pool),
		Publisher:          queue.NewPublisher(conn.Queue),
		Deduplicator:       queue.NewDeduplicator(conn.Redis, cfg.RabbitMQ.DedupTTL),
		JWT:                jwt,
		SessionRevocations: sessionRevocations,
		SSE:                sse.NewHub(32),
		Authenticator:      middleware.NewAuthenticator(jwt, sessionRevocations),
		ClientAuthorizer:   middleware.NewClientAuthorizer(repository.NewClientRepository(conn.Pool)),
		SMS:                newSMSSender(cfg.SMS),
	}
	auth, err := newAuthModule(c)
	if err != nil {
		return nil, err
	}
	c.Health = newHealthModule(c)
	c.Auth = auth
	c.Notifications = newNotificationsModule(c)
	return c, nil
}

func (c *Container) OutboxRelay() *outbox.Relay {
	return outbox.NewRelay(c.Pool, c.Publisher, c.Config.Outbox.PollInterval, c.Config.Outbox.BatchSize)
}

func (c *Container) Consumers() []*queue.Consumer {
	consumers := make([]*queue.Consumer, 0)
	for _, cc := range c.consumerConfigs() {
		cc.Prefetch = c.Config.RabbitMQ.Prefetch
		cc.MaxRetries = c.Config.RabbitMQ.MaxRetries
		cc.RetryDelay = c.Config.RabbitMQ.RetryDelay
		consumers = append(consumers, queue.NewConsumer(c.Queue, c.Deduplicator, cc))
	}
	return consumers
}

func (c *Container) consumerConfigs() []queue.ConsumerConfig {
	return c.Notifications.Consumers()
}

func newSMSSender(cfg config.SMSConfig) services.ISMSSender {
	if !cfg.Enabled() {
		return sms.LogSender{}
	}
	return sms.NewCastBrickSender(cfg.APIKey, cfg.BaseURL, cfg.SenderID, cfg.Timeout)
}

func (c *Container) Close() error {
	var errs []error
	c.Publisher.Close()
	if err := c.Queue.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close rabbitmq: %w", err))
	}
	if err := c.Redis.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close redis: %w", err))
	}
	c.Pool.Close()
	return errors.Join(errs...)
}
