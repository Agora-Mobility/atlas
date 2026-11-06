package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var (
	ErrPlateTaken   = errors.New("this fleet already has a vehicle with this plate")
	ErrVehicleTaken = errors.New("this vehicle is not available")
)

type FleetDriver struct {
	UserID             uuid.UUID
	Name               string
	Phone              string
	IsVerified         bool
	Status             entities.ProviderStatus
	RequestedServiceID *uuid.UUID
	ServiceID          *uuid.UUID
	ServiceName        string
	VehicleID          *uuid.UUID
	VehiclePlate       string
}

type VehicleFilter struct {
	OrganizationID *uuid.UUID
	Kind           string
	Search         string
	Limit          int
	Offset         int
}

type IFleetRepository interface {
	ListDrivers(ctx context.Context, organizationID uuid.UUID, status string, limit, offset int) ([]FleetDriver, int64, error)
	CreateVehicle(ctx context.Context, vehicle *entities.FleetVehicle) error
	UpdateVehicle(ctx context.Context, vehicle *entities.FleetVehicle) error
	FindVehicle(ctx context.Context, id uuid.UUID) (*entities.FleetVehicle, error)
	ListVehicles(ctx context.Context, filter VehicleFilter) ([]entities.FleetVehicle, int64, error)
	AssignVehicle(ctx context.Context, vehicleID, providerID uuid.UUID) error
	ReleaseVehicle(ctx context.Context, providerID uuid.UUID) error
	SaveWorkshop(ctx context.Context, workshop *entities.Workshop) error
	FindWorkshop(ctx context.Context, id uuid.UUID) (*entities.Workshop, error)
	ListWorkshops(ctx context.Context, organizationID *uuid.UUID, limit, offset int) ([]entities.Workshop, int64, error)
	SaveWorkshopService(ctx context.Context, service *entities.WorkshopService) error
	FindWorkshopService(ctx context.Context, id uuid.UUID) (*entities.WorkshopService, error)
}
