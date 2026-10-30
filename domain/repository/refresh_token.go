package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type IRefreshTokenRepository interface {
	Create(ctx context.Context, token *entities.RefreshToken) error
	FindByHashForUpdate(ctx context.Context, tokenHash string) (*entities.RefreshToken, error)
	MarkRotated(ctx context.Context, token *entities.RefreshToken) error
	RevokeFamily(ctx context.Context, familyID uuid.UUID, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) error
	// ListSessionsIssuedSince returns the family (session) ids with a token
	// issued at or after since, revoked or not.
	ListSessionsIssuedSince(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error)
}
