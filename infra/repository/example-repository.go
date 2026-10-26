// Package repository implements the domain repository interfaces on top of
// the SQLC-generated queries.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/valueobjects"
	"github.com/Agora-Mobility/atlas/infra/cache"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

const exampleCacheTTL = 5 * time.Minute

type ExampleRepository struct {
	pool  *pgxpool.Pool
	cache cache.Cache
}

var _ repository.IExampleRepository = (*ExampleRepository)(nil)

// NewExampleRepository builds the repository. cache may be nil.
func NewExampleRepository(pool *pgxpool.Pool, c cache.Cache) *ExampleRepository {
	return &ExampleRepository{pool: pool, cache: c}
}

func exampleCacheKey(id uuid.UUID) string { return "examples:" + id.String() }

func (r *ExampleRepository) Create(ctx context.Context, e *entities.Example) error {
	_, err := postgres.Queries(ctx, r.pool).CreateExample(ctx, gen.CreateExampleParams{
		ID:          e.ID,
		Name:        e.Name,
		Description: e.Description,
		Longitude:   e.Location.Longitude,
		Latitude:    e.Location.Latitude,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert example: %w", err)
	}
	return nil
}

func (r *ExampleRepository) FindByID(ctx context.Context, id uuid.UUID) (*entities.Example, error) {
	if r.cache != nil {
		var cached entities.Example
		if hit, err := r.cache.Get(ctx, exampleCacheKey(id), &cached); err != nil {
			logger.Warn(ctx, "example cache read failed", zap.Error(err))
		} else if hit {
			return &cached, nil
		}
	}

	row, err := postgres.Queries(ctx, r.pool).GetExampleByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, customerrors.NotFound("example")
	}
	if err != nil {
		return nil, fmt.Errorf("get example: %w", err)
	}

	e := toExample(gen.ListExamplesRow(row))
	if r.cache != nil {
		if err := r.cache.Set(ctx, exampleCacheKey(id), e, exampleCacheTTL); err != nil {
			logger.Warn(ctx, "example cache write failed", zap.Error(err))
		}
	}
	return e, nil
}

func (r *ExampleRepository) List(ctx context.Context, params pagination.Params) ([]*entities.Example, int64, error) {
	q := postgres.Queries(ctx, r.pool)

	total, err := q.CountExamples(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count examples: %w", err)
	}
	rows, err := q.ListExamples(ctx, gen.ListExamplesParams{PageLimit: params.Limit(), PageOffset: params.Offset()})
	if err != nil {
		return nil, 0, fmt.Errorf("list examples: %w", err)
	}

	items := make([]*entities.Example, len(rows))
	for i, row := range rows {
		items[i] = toExample(row)
	}
	return items, total, nil
}

// toExample maps a row to the entity. Timestamps are normalised to UTC so the
// API never mixes time zones (pgx returns them in the server's local zone).
func toExample(row gen.ListExamplesRow) *entities.Example {
	return &entities.Example{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Location:    valueobjects.GeoPoint{Latitude: row.Latitude, Longitude: row.Longitude},
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
}
