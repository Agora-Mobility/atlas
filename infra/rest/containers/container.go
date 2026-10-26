// Package containers wires dependencies by hand. It is the only place that
// receives the *pgxpool.Pool and turns it into repositories.
package containers

import (
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/cache"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/queue"
	authjwt "github.com/Agora-Mobility/atlas/infra/security/auth-jwt"
	"github.com/Agora-Mobility/atlas/infra/sse"
)

type Container struct {
	Config *config.Config

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

	Health  *HealthModule
	Example *ExampleModule
}

func New(cfg *config.Config, version string, pool *pgxpool.Pool, rdb *goredis.Client, mgr *queue.Manager) *Container {
	c := &Container{
		Config:       cfg,
		Pool:         pool,
		Redis:        rdb,
		Queue:        mgr,
		Cache:        cache.NewRedisCache(rdb, cfg.App.Name+":cache:"),
		TxManager:    postgres.NewTxManager(pool),
		Outbox:       outbox.NewRepository(pool),
		Publisher:    queue.NewPublisher(mgr),
		Deduplicator: queue.NewDeduplicator(rdb, cfg.RabbitMQ.DedupTTL),
		JWT:          authjwt.NewService(cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.AccessTTL),
		SSE:          sse.NewHub(32),
	}
	c.Health = newHealthModule(c, version)
	c.Example = newExampleModule(c)
	return c
}

// OutboxRelay builds the relay that publishes this process's outbox.
func (c *Container) OutboxRelay() *outbox.Relay {
	return outbox.NewRelay(c.Pool, c.Publisher, c.Config.Outbox.PollInterval, c.Config.Outbox.BatchSize)
}

// Consumers returns every RabbitMQ consumer of every module.
func (c *Container) Consumers() []*queue.Consumer {
	var cfgs []queue.ConsumerConfig
	cfgs = append(cfgs, c.Example.Consumers()...)

	consumers := make([]*queue.Consumer, len(cfgs))
	for i, cc := range cfgs {
		cc.Prefetch = c.Config.RabbitMQ.Prefetch
		cc.MaxRetries = c.Config.RabbitMQ.MaxRetries
		cc.RetryDelay = c.Config.RabbitMQ.RetryDelay
		consumers[i] = queue.NewConsumer(c.Queue, c.Deduplicator, cc)
	}
	return consumers
}
