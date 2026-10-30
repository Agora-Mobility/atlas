package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

// UserFilter narrows ListUsers. Empty fields do not filter.
type UserFilter struct {
	Roles  []enums.Role
	Status enums.UserStatus
	// Search matches part of the name, e-mail or phone number.
	Search string
}

type IUserRepository interface {
	Create(ctx context.Context, user *entities.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*entities.User, error)
	FindByEmail(ctx context.Context, email string) (*entities.User, error)
	FindByPhone(ctx context.Context, phone string) (*entities.User, error)
	Update(ctx context.Context, user *entities.User) error
	UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	SoftDelete(ctx context.Context, id uuid.UUID, at time.Time) error
	List(ctx context.Context, filter UserFilter, page pagination.Params) ([]*entities.User, int64, error)
}
