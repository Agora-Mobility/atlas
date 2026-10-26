package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

// IExampleRepository persists examples. FindByID returns a NotFound AppError
// when the example does not exist.
type IExampleRepository interface {
	Create(ctx context.Context, example *entities.Example) error
	FindByID(ctx context.Context, id uuid.UUID) (*entities.Example, error)
	List(ctx context.Context, params pagination.Params) ([]*entities.Example, int64, error)
}
