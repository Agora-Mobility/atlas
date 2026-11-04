package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrCustomerPlateTaken = errors.New("this plate is already in the vehicles of the customer")

type IGarageRepository interface {
	CreateVehicle(ctx context.Context, vehicle *entities.CustomerVehicle) error
	UpdateVehicle(ctx context.Context, vehicle *entities.CustomerVehicle, now time.Time) (bool, error)
	DeactivateVehicle(ctx context.Context, id, customerID uuid.UUID, now time.Time) (bool, error)
	Vehicle(ctx context.Context, id, customerID uuid.UUID) (*entities.CustomerVehicle, error)
	Vehicles(ctx context.Context, customerID uuid.UUID) ([]entities.CustomerVehicle, error)
	CreateUpload(ctx context.Context, upload *entities.MediaUpload) error
	Uploads(ctx context.Context, ids []uuid.UUID) ([]entities.MediaUpload, error)
	Upload(ctx context.Context, id uuid.UUID) (*entities.MediaUpload, error)
	AttachToRequest(ctx context.Context, ids []uuid.UUID, ownerID, requestID uuid.UUID) (int64, error)
	OfRequest(ctx context.Context, requestID uuid.UUID) ([]entities.MediaUpload, error)
}
