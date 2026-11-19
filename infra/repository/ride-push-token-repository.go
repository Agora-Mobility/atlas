package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type RidePushTokenRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRidePushTokenRepository = (*RidePushTokenRepository)(nil)

func NewRidePushTokenRepository(pool *pgxpool.Pool) *RidePushTokenRepository {
	return &RidePushTokenRepository{pool: pool}
}

func (r *RidePushTokenRepository) Save(ctx context.Context, token entities.RidePushToken) error {
	err := postgres.Queries(ctx, r.pool).UpsertRidePushToken(ctx, gen.UpsertRidePushTokenParams{
		RequestID: token.RequestID, Platform: token.Platform, UserID: token.UserID, Token: token.Token,
	})
	if err != nil {
		return fmt.Errorf("save ride push token: %w", err)
	}
	return nil
}

func (r *RidePushTokenRepository) OfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.RidePushToken, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListRidePushTokens(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("list ride push tokens: %w", err)
	}
	out := make([]entities.RidePushToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.RidePushToken{RequestID: row.RequestID, Platform: row.Platform, UserID: row.UserID, Token: row.Token})
	}
	return out, nil
}

func (r *RidePushTokenRepository) DeleteOfRequest(ctx context.Context, requestID uuid.UUID) error {
	if err := postgres.Queries(ctx, r.pool).DeleteRidePushTokens(ctx, requestID); err != nil {
		return fmt.Errorf("delete ride push tokens: %w", err)
	}
	return nil
}

func (r *RidePushTokenRepository) Remove(ctx context.Context, token string) error {
	if err := postgres.Queries(ctx, r.pool).DeleteRidePushToken(ctx, token); err != nil {
		return fmt.Errorf("remove ride push token: %w", err)
	}
	return nil
}
