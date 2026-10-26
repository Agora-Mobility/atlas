// Package postgres owns the PostgreSQL connection pool, transactions,
// migrations and seeders.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/utils"
)

const connectAttempts = 5

// Connection creates a pgx pool and waits until the database answers a ping,
// retrying with backoff so the API survives a database that starts slowly.
func Connection(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	for attempt := 0; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if attempt+1 >= connectAttempts {
			pool.Close()
			return nil, fmt.Errorf("ping postgres: %w", err)
		}
		logger.Warn(ctx, "postgres not ready, retrying", zap.Int("attempt", attempt+1), zap.Error(err))
		if !utils.Sleep(ctx, utils.Backoff(attempt, 500*time.Millisecond, 5*time.Second)) {
			pool.Close()
			return nil, ctx.Err()
		}
	}
}
