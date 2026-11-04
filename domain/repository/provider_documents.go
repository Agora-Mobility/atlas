package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrRejectionReasonCodeTaken = errors.New("this scope already has a reason with this code")

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
	OrganizationID       *uuid.UUID
	OrganizationName     string
	SubmittedAt          *time.Time
}

type IProviderDocumentRepository interface {
	OpenServices(ctx context.Context) ([]OpenService, error)
	Requirements(ctx context.Context, serviceID uuid.UUID) (*entities.ProviderRequirements, error)
	DocumentType(ctx context.Context, code string) (*entities.DocumentType, error)
	Documents(ctx context.Context, providerID uuid.UUID) ([]*entities.ProviderDocument, error)
	Document(ctx context.Context, providerID uuid.UUID, documentType string) (*entities.ProviderDocument, error)
	ListReasons(ctx context.Context, scope string, includeInactive bool) ([]entities.RejectionReason, error)
	ActiveReasonsByIDs(ctx context.Context, scope entities.RejectionScope, ids []uuid.UUID) ([]entities.RejectionReason, error)
	FindReason(ctx context.Context, id uuid.UUID) (*entities.RejectionReason, error)
	CreateReason(ctx context.Context, reason *entities.RejectionReason) error
	UpdateReason(ctx context.Context, reason *entities.RejectionReason) error
	ReplaceProviderReasons(ctx context.Context, providerID uuid.UUID, reasonIDs []uuid.UUID) error
	ProviderReasons(ctx context.Context, providerID uuid.UUID) ([]entities.RejectionReason, error)
	ReplaceDocumentReasons(ctx context.Context, providerID uuid.UUID, documentType string, reasonIDs []uuid.UUID) error
	DocumentReasons(ctx context.Context, providerID uuid.UUID) (map[string][]entities.RejectionReason, error)
	SaveDocument(ctx context.Context, document *entities.ProviderDocument) error
}

type ProviderApplicationFilter struct {
	Status         string
	OrganizationID *uuid.UUID
	ServiceID      *uuid.UUID
	Category       string
	Search         string
	Limit          int
	Offset         int
}
