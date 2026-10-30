//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
)

const postgisImage = "postgis/postgis:18-3.6-alpine"

func setupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	app, _ := setupDBs(t)
	return app
}

func setupDBs(t *testing.T) (app, owner *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, postgisImage,
		tcpostgres.WithDatabase("atlas_test"),
		tcpostgres.WithUsername("atlas"),
		tcpostgres.WithPassword("atlas"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(url, "up"))

	cfg := config.DatabaseConfig{URL: url, MaxConns: 5, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute}
	app, err = postgres.Connection(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(app.Close)
	owner, err = postgres.OwnerConnection(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(owner.Close)
	return app, owner
}
