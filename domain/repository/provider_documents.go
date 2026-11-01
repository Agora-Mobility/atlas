package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

type OpenService struct {
	ID              uuid.UUID
	Slug            string
	Name            string
	Description     string
	CategoryID      uuid.UUID
	CategorySlug    string
	CategoryName    string
	RequiresVehicle bool
	MinVehicleSeats int
}

type ProviderApplication struct {
	UserID               uuid.UUID
	Name                 string
	Phone                string
	Status               entities.ProviderStatus
	RequestedServiceID   *uuid.UUID
	RequestedServiceName string
	ServiceID            *uuid.UUID
	VehiclePlate         string
	SubmittedAt          *time.Time
}

type IProviderDocumentRepository interface {
	OpenServices(ctx context.Context) ([]OpenService, error)
	Requirements(ctx context.Context, serviceID uuid.UUID) (*entities.ProviderRequirements, error)
	DocumentType(ctx context.Context, code string) (*entities.DocumentType, error)
	Documents(ctx context.Context, providerID uuid.UUID) ([]*entities.ProviderDocument, error)
	Document(ctx context.Context, providerID uuid.UUID, documentType string) (*entities.ProviderDocument, error)
	SaveDocument(ctx context.Context, document *entities.ProviderDocument) error
}
