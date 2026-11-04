package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

var ErrCatalogCodeTaken = errors.New("this code already exists for this kind and category")

type IReferenceRepository interface {
	Items(ctx context.Context, kind entities.CatalogKind, category string, includeInactive bool) ([]entities.CatalogItem, error)
	Item(ctx context.Context, id uuid.UUID) (*entities.CatalogItem, error)
	ItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]entities.CatalogItem, error)
	CreateItem(ctx context.Context, item *entities.CatalogItem) error
	UpdateItem(ctx context.Context, item *entities.CatalogItem) error
	Setting(ctx context.Context, key string) (string, error)
	Settings(ctx context.Context) (map[string]string, error)
	SetSetting(ctx context.Context, key, value string) error
}

type IRatingRepository interface {
	Create(ctx context.Context, rating *entities.Rating) (bool, error)
	OfRequest(ctx context.Context, requestID uuid.UUID) (*entities.Rating, error)
	OfProvider(ctx context.Context, providerID uuid.UUID, limit, offset int) ([]entities.Rating, int64, error)
}
