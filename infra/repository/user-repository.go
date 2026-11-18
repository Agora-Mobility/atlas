package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

const uniqueViolation = "23505"

var conflictByConstraint = map[string]error{
	"users_email_key":             repository.ErrEmailAlreadyExists,
	"users_phone_key":             repository.ErrPhoneAlreadyExists,
	"organizations_nif_key":       repository.ErrNIFAlreadyExists,
	"roles_organization_code_key": repository.ErrRoleCodeTaken,
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

func (r *UserRepository) SoftDelete(ctx context.Context, id uuid.UUID, at time.Time) error {
	rows, err := postgres.Queries(ctx, r.pool).SoftDeleteUser(ctx, gen.SoftDeleteUserParams{ID: id, DeletedAt: &at})
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	if rows == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (r *UserRepository) List(ctx context.Context, filter repository.UserFilter, page pagination.Params) ([]*entities.User, int64, error) {
	q := postgres.Queries(ctx, r.pool)
	roles, status, search := filterArgs(filter)

	total, err := q.CountUsers(ctx, gen.CountUsersParams{Roles: roles, Status: status, Category: filter.Category, Search: search})
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := q.ListUsers(ctx, gen.ListUsersParams{
		Roles: roles, Status: status, Category: filter.Category, Search: search,
		PageLimit: page.Limit(), PageOffset: page.Offset(),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}

	users := make([]*entities.User, 0, len(rows))
	for i := range rows {
		users = append(users, toUser(rows[i]))
	}
	return users, total, nil
}

func filterArgs(filter repository.UserFilter) ([]string, *enums.UserStatus, *string) {
	roles := make([]string, 0, len(filter.Roles))
	for _, role := range filter.Roles {
		roles = append(roles, role.String())
	}
	var status *enums.UserStatus
	if filter.Status != "" {
		status = &filter.Status
	}
	return roles, status, searchPattern(filter.Search)
}

func searchPattern(term string) *string {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil
	}
	pattern := "%" + likeEscaper.Replace(term) + "%"
	return &pattern
}

// likeEscaper keeps user input literal inside a LIKE pattern (default escape \).
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

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
