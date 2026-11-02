package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type RequestShare struct {
	ID        uuid.UUID
	RequestID uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type ILiveRepository interface {
	AddMessage(ctx context.Context, message *entities.RequestMessage) error
	Messages(ctx context.Context, requestID uuid.UUID, limit, offset int) ([]entities.RequestMessage, error)
	MarkRead(ctx context.Context, requestID, readerID uuid.UUID, at time.Time) error
	CreateShare(ctx context.Context, id, requestID uuid.UUID, tokenHash string, expiresAt, now time.Time) error
	ShareByHash(ctx context.Context, tokenHash string) (*RequestShare, error)
	RevokeShares(ctx context.Context, requestID uuid.UUID, at time.Time) error
	CreateSOS(ctx context.Context, alert *entities.SOSAlert) (bool, error)
	OpenSOS(ctx context.Context, requestID, userID uuid.UUID) (*entities.SOSAlert, error)
	ListSOS(ctx context.Context, status string, limit, offset int) ([]entities.SOSAlert, int64, error)
	ResolveSOS(ctx context.Context, id uuid.UUID, by *uuid.UUID, at time.Time) (bool, error)
}
