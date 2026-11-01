package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type IAddressRepository interface {
	ListOfUser(ctx context.Context, userID uuid.UUID) ([]entities.UserAddress, error)
	FindOfUser(ctx context.Context, userID, addressID uuid.UUID) (*entities.UserAddress, error)
	SaveForUser(ctx context.Context, userID uuid.UUID, address entities.UserAddress) error
	Delete(ctx context.Context, addressID uuid.UUID) error
}
