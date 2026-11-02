package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type RatingRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRatingRepository = (*RatingRepository)(nil)

func NewRatingRepository(pool *pgxpool.Pool) *RatingRepository {
	return &RatingRepository{pool: pool}
}

func (r *RatingRepository) Create(ctx context.Context, rating *entities.Rating) (bool, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.InsertRating(ctx, gen.InsertRatingParams{
		ID: rating.ID, RequestID: rating.RequestID, CustomerID: rating.CustomerID, ProviderID: rating.ProviderID, Stars: int16(rating.Stars), // #nosec G115 -- 1 to 5
		ComplimentIds: rating.ComplimentIDs, Comment: rating.Comment, CreatedAt: rating.CreatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("create rating: %w", err)
	}
	if rows == 0 {
		return false, nil
	}
	if rating.ProviderID != nil {
		if err := queries.AddProviderRating(ctx, gen.AddProviderRatingParams{Stars: int64(rating.Stars), ProviderID: *rating.ProviderID}); err != nil {
			return false, fmt.Errorf("add provider rating: %w", err)
		}
	}
	return true, nil
}

func (r *RatingRepository) OfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Rating, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRatingOfRequest(ctx, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rating: %w", err)
	}
	return &entities.Rating{ID: row.ID, RequestID: row.RequestID, CustomerID: row.CustomerID, ProviderID: row.ProviderID, Stars: int(row.Stars), ComplimentIDs: row.ComplimentIds, Comment: row.Comment, CreatedAt: row.CreatedAt}, nil
}

func (r *RatingRepository) OfProvider(ctx context.Context, providerID uuid.UUID, limit, offset int) ([]entities.Rating, int64, error) {
	queries := postgres.Queries(ctx, r.pool)
	rows, err := queries.ListProviderRatings(ctx, gen.ListProviderRatingsParams{ProviderID: &providerID, PageLimit: int32(limit), PageOffset: int32(offset)}) // #nosec G115 -- bounded by pagination
	if err != nil {
		return nil, 0, fmt.Errorf("list provider ratings: %w", err)
	}
	total, err := queries.CountProviderRatings(ctx, &providerID)
	if err != nil {
		return nil, 0, fmt.Errorf("count provider ratings: %w", err)
	}
	out := make([]entities.Rating, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, entities.Rating{ID: row.ID, RequestID: row.RequestID, Stars: int(row.Stars), ComplimentIDs: row.ComplimentIds, Comment: row.Comment, CreatedAt: row.CreatedAt})
	}
	return out, total, nil
}
