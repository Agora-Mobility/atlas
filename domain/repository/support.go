package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type ISupportRepository interface {
	NextCode(ctx context.Context) (string, error)
	Create(ctx context.Context, ticket *entities.SupportTicket) error
	Find(ctx context.Context, id uuid.UUID) (*entities.SupportTicket, error)
	OfUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]entities.SupportTicket, int64, error)
	List(ctx context.Context, status string, limit, offset int) ([]entities.SupportTicket, int64, error)
	SetStatus(ctx context.Context, id uuid.UUID, status string) error
	AddMessage(ctx context.Context, message *entities.SupportMessage) error
	Messages(ctx context.Context, ticketID uuid.UUID) ([]entities.SupportMessage, error)
}
