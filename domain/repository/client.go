package repository

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type IClientRepository interface {
	FindActiveByClientID(ctx context.Context, clientID string) (*entities.Client, error)
}
