// Package postgres owns the PostgreSQL connection pool, transactions,
// migrations and seeders.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/utils"
)

const connectAttempts = 5

const AppRole = "atlas_app"

// Connection creates the pool of the API. Every connection switches to
// AppRole, which owns nothing and is subject to row-level security, and waits
// until the database answers a ping, retrying with backoff so the API
// survives a database that starts slowly.
func Connection(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	return connect(ctx, cfg, true)
}

// OwnerConnection keeps the role of DATABASE_URL, for tools that maintain
// the data (seed) rather than serve requests.
func OwnerConnection(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	return connect(ctx, cfg, false)
}

func connect(ctx context.Context, cfg config.DatabaseConfig, asApp bool) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	// Every connection resolves our objects in Schema, whatever the URL says.
	poolCfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	if asApp {
		poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if _, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{AppRole}.Sanitize()); err != nil {
				return fmt.Errorf("switch to role %s (run the migrations first): %w", AppRole, err)
			}
			return nil
		}
	}

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
		logger.FromContext(ctx).Warn("postgres not ready, retrying", zap.Int("attempt", attempt+1), zap.Error(err))
		if !utils.Sleep(ctx, utils.Backoff(attempt, 500*time.Millisecond, 5*time.Second)) {
			pool.Close()
			return nil, ctx.Err()
		}
	}
}
