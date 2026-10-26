//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/outbox"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

// postgisImage must match docker-compose.yml.
const postgisImage = "postgis/postgis:18-3.6-alpine"

func setupDB(t *testing.T) *pgxpool.Pool {
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

	pool, err := postgres.Connection(ctx, config.DatabaseConfig{
		URL: url, MaxConns: 5, MinConns: 1, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestExampleRepository(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	repo := repository.NewExampleRepository(pool, nil)

	created, err := entities.NewExample("Ilha do Cabo", "integration", -8.79, 13.22)
	require.NoError(t, err)
	require.NoError(t, repo.Create(ctx, created))

	t.Run("find by id round-trips PostGIS coordinates", func(t *testing.T) {
		got, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created.Name, got.Name)
		assert.InDelta(t, -8.79, got.Location.Latitude, 1e-9)
		assert.InDelta(t, 13.22, got.Location.Longitude, 1e-9)
		assert.Equal(t, time.UTC, got.CreatedAt.Location())
	})

	t.Run("missing id returns NotFound", func(t *testing.T) {
		_, err := repo.FindByID(ctx, uuid.New())
		assert.Equal(t, customerrors.CodeNotFound, customerrors.As(err).Code)
	})

	t.Run("list paginates", func(t *testing.T) {
		items, total, err := repo.List(ctx, pagination.Params{Page: 1, Size: 10})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		assert.Len(t, items, 1)
	})
}

func TestTransactionRollsBackEntityAndOutboxTogether(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	repo := repository.NewExampleRepository(pool, nil)
	outboxRepo := outbox.NewRepository(pool)
	txm := postgres.NewTxManager(pool)

	e, err := entities.NewExample("Rollback", "", 0, 0)
	require.NoError(t, err)
	env, err := events.NewEnvelope(events.ExampleCreatedV1{ID: e.ID, Name: e.Name}, "corr")
	require.NoError(t, err)

	boom := errors.New("fail after writes")
	err = txm.WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, repo.Create(ctx, e))
		require.NoError(t, outboxRepo.Save(ctx, env))
		return boom
	})
	require.ErrorIs(t, err, boom)

	_, err = repo.FindByID(ctx, e.ID)
	assert.Equal(t, customerrors.CodeNotFound, customerrors.As(err).Code)

	var outboxRows int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox").Scan(&outboxRows))
	assert.Zero(t, outboxRows)

	// And the happy path commits both.
	require.NoError(t, txm.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, e); err != nil {
			return err
		}
		return outboxRepo.Save(ctx, env)
	}))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE published_at IS NULL").Scan(&outboxRows))
	assert.Equal(t, 1, outboxRows)
}
