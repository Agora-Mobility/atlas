package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type RefreshTokenRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IRefreshTokenRepository = (*RefreshTokenRepository)(nil)

func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, t *entities.RefreshToken) error {
	err := postgres.Queries(ctx, r.pool).CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
		ID:           t.ID,
		UserID:       t.UserID,
		ClientID:     t.ClientID,
		FamilyID:     t.FamilyID,
		TokenHash:    t.TokenHash,
		ExpiresAt:    t.ExpiresAt,
		RevokedAt:    t.RevokedAt,
		ReplacedByID: t.ReplacedByID,
		UserAgent:    t.UserAgent,
		IpAddress:    t.IPAddress,
		CreatedAt:    t.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) FindByHashForUpdate(ctx context.Context, tokenHash string) (*entities.RefreshToken, error) {
	row, err := postgres.Queries(ctx, r.pool).GetRefreshTokenByHashForUpdate(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	return toRefreshToken(row), nil
}

func (r *RefreshTokenRepository) MarkRotated(ctx context.Context, t *entities.RefreshToken) error {
	err := postgres.Queries(ctx, r.pool).RevokeRefreshToken(ctx, gen.RevokeRefreshTokenParams{
		ID:           t.ID,
		RevokedAt:    t.RevokedAt,
		ReplacedByID: t.ReplacedByID,
	})
	if err != nil {
		return fmt.Errorf("rotate refresh token: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).RevokeRefreshTokenFamily(ctx, gen.RevokeRefreshTokenFamilyParams{
		FamilyID: familyID, RevokedAt: &at,
	})
	if err != nil {
		return fmt.Errorf("revoke refresh token family: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).RevokeUserRefreshTokens(ctx, gen.RevokeUserRefreshTokensParams{
		UserID: userID, RevokedAt: &at,
	})
	if err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}

func toRefreshToken(row gen.RefreshToken) *entities.RefreshToken {
	return &entities.RefreshToken{
		ID:           row.ID,
		UserID:       row.UserID,
		ClientID:     row.ClientID,
		FamilyID:     row.FamilyID,
		TokenHash:    row.TokenHash,
		ExpiresAt:    row.ExpiresAt.UTC(),
		RevokedAt:    utcOrNil(row.RevokedAt),
		ReplacedByID: row.ReplacedByID,
		UserAgent:    row.UserAgent,
		IPAddress:    row.IpAddress,
		CreatedAt:    row.CreatedAt.UTC(),
	}
}
