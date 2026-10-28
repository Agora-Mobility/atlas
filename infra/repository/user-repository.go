package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

const uniqueViolation = "23505"

var conflictByConstraint = map[string]error{
	"users_email_key": repository.ErrEmailAlreadyExists,
	"users_phone_key": repository.ErrPhoneAlreadyExists,
}

type UserRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IUserRepository = (*UserRepository)(nil)

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, u *entities.User) error {
	_, err := postgres.Queries(ctx, r.pool).CreateUser(ctx, gen.CreateUserParams{
		ID:          u.ID,
		Name:        u.Name,
		Email:       nullableString(u.Email),
		Password:    u.Password,
		Phone:       nullableString(u.Phone),
		IsVerified:  u.IsVerified,
		Status:      u.Status,
		Role:        u.Role,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		DeletedAt:   u.DeletedAt,
	})
	if err != nil {
		return userWriteError("insert user", err)
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*entities.User, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserByID(ctx, id)
	return userOrNil(row, err)
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*entities.User, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserByEmail(ctx, email)
	return userOrNil(row, err)
}

func (r *UserRepository) FindByPhone(ctx context.Context, phone string) (*entities.User, error) {
	row, err := postgres.Queries(ctx, r.pool).GetUserByPhone(ctx, &phone)
	return userOrNil(row, err)
}

func (r *UserRepository) Update(ctx context.Context, u *entities.User) error {
	_, err := postgres.Queries(ctx, r.pool).UpdateUser(ctx, gen.UpdateUserParams{
		ID:         u.ID,
		Name:       u.Name,
		Email:      nullableString(u.Email),
		Password:   u.Password,
		Phone:      nullableString(u.Phone),
		IsVerified: u.IsVerified,
		Status:     u.Status,
		Role:       u.Role,
		UpdatedAt:  u.UpdatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	if err != nil {
		return userWriteError("update user", err)
	}
	return nil
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	err := postgres.Queries(ctx, r.pool).UpdateUserLastLogin(ctx, gen.UpdateUserLastLoginParams{ID: id, LastLoginAt: &at})
	if err != nil {
		return fmt.Errorf("update user last login: %w", err)
	}
	return nil
}

func userOrNil(row gen.User, err error) (*entities.User, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return toUser(row), nil
}

func userWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		if conflict, ok := conflictByConstraint[pgErr.ConstraintName]; ok {
			return conflict
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func toUser(row gen.User) *entities.User {
	return &entities.User{
		Base: entities.Base{
			ID:        row.ID,
			CreatedAt: row.CreatedAt.UTC(),
			UpdatedAt: row.UpdatedAt.UTC(),
			DeletedAt: utcOrNil(row.DeletedAt),
		},
		Name:        row.Name,
		Email:       valueOrEmpty(row.Email),
		Password:    row.Password,
		Phone:       valueOrEmpty(row.Phone),
		IsVerified:  row.IsVerified,
		Status:      row.Status,
		Role:        row.Role,
		LastLoginAt: utcOrNil(row.LastLoginAt),
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
