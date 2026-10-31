package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

type ServiceFilter struct {
	CategoryID *uuid.UUID
	IsActive   *bool
	Search     string
}

type ICatalogRepository interface {
	ListCategories(ctx context.Context, onlyActive bool) ([]*entities.ServiceCategory, error)
	// FindCategory returns nil when the category does not exist.
	FindCategory(ctx context.Context, id uuid.UUID) (*entities.ServiceCategory, error)
	CreateCategory(ctx context.Context, category *entities.ServiceCategory) error
	UpdateCategory(ctx context.Context, category *entities.ServiceCategory) error

	ListServices(ctx context.Context, filter ServiceFilter, page pagination.Params) ([]*entities.Service, int64, error)
	// ListOfferedServices returns the active services of active categories.
	ListOfferedServices(ctx context.Context) ([]*entities.Service, error)
	// FindService returns nil when the service does not exist.
	FindService(ctx context.Context, id uuid.UUID) (*entities.Service, error)
	CreateService(ctx context.Context, service *entities.Service) error
	UpdateService(ctx context.Context, service *entities.Service) error
}
