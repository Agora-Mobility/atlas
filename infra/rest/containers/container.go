package containers

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/cache"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/redis"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/queue"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/infra/sse"
)

type Container struct {
	Config  *config.Config
	Version string

	Pool  *pgxpool.Pool
	Redis *goredis.Client
	Queue *queue.Manager

	Cache        cache.Cache
	TxManager    *postgres.TxManager
	Outbox       *outbox.Repository
	Publisher    *queue.Publisher
	Deduplicator *queue.Deduplicator
	JWT          *authjwt.Service
	SSE          *sse.Hub

	Health *HealthModule
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

func New(cfg *config.Config, version string, conn *Connections) *Container {
	jwt := authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.AccessTTL)
	c := &Container{
		Config:       cfg,
		Version:      version,
		Pool:         conn.Pool,
		Redis:        conn.Redis,
		Queue:        conn.Queue,
		Cache:        cache.NewRedisCache(conn.Redis, cfg.App.Name+":cache:"),
		TxManager:    postgres.NewTxManager(conn.Pool),
		Outbox:       outbox.NewRepository(conn.Pool),
		Publisher:    queue.NewPublisher(conn.Queue),
		Deduplicator: queue.NewDeduplicator(conn.Redis, cfg.RabbitMQ.DedupTTL),
		JWT:          jwt,
		SSE:          sse.NewHub(32),
	}
	c.Health = newHealthModule(c)
	return c
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
	return nil
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
