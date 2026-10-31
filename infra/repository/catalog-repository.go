package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
	"github.com/Agora-Mobility/atlas/shared/pagination"
)

const foreignKeyViolation = "23503"

type CatalogRepository struct {
	pool *pgxpool.Pool
}

var _ repository.ICatalogRepository = (*CatalogRepository)(nil)

func NewCatalogRepository(pool *pgxpool.Pool) *CatalogRepository {
	return &CatalogRepository{pool: pool}
}

func (r *CatalogRepository) ListCategories(ctx context.Context, onlyActive bool) ([]*entities.ServiceCategory, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListServiceCategories(ctx, onlyActive)
	if err != nil {
		return nil, fmt.Errorf("list service categories: %w", err)
	}
	categories := make([]*entities.ServiceCategory, 0, len(rows))
	for i := range rows {
		categories = append(categories, toServiceCategory(rows[i]))
	}
	return categories, nil
}

func (r *CatalogRepository) FindCategory(ctx context.Context, id uuid.UUID) (*entities.ServiceCategory, error) {
	row, err := postgres.Queries(ctx, r.pool).GetServiceCategory(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get service category: %w", err)
	}
	return toServiceCategory(row), nil
}

func (r *CatalogRepository) CreateCategory(ctx context.Context, c *entities.ServiceCategory) error {
	err := postgres.Queries(ctx, r.pool).CreateServiceCategory(ctx, gen.CreateServiceCategoryParams{
		ID:           c.ID,
		Slug:         c.Slug,
		Name:         c.Name,
		Description:  c.Description,
		Icon:         c.Icon,
		DisplayOrder: int32(c.DisplayOrder), // #nosec G115 -- bounded by the entity
		IsActive:     c.IsActive,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	})
	return catalogWriteError("insert service category", err)
}

func (r *CatalogRepository) UpdateCategory(ctx context.Context, c *entities.ServiceCategory) error {
	err := postgres.Queries(ctx, r.pool).UpdateServiceCategory(ctx, gen.UpdateServiceCategoryParams{
		ID:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		Icon:         c.Icon,
		DisplayOrder: int32(c.DisplayOrder), // #nosec G115 -- bounded by the entity
		IsActive:     c.IsActive,
		UpdatedAt:    c.UpdatedAt,
	})
	return catalogWriteError("update service category", err)
}

func (r *CatalogRepository) ListServices(ctx context.Context, filter repository.ServiceFilter, page pagination.Params) ([]*entities.Service, int64, error) {
	q := postgres.Queries(ctx, r.pool)
	search := searchPattern(filter.Search)

	total, err := q.CountServices(ctx, gen.CountServicesParams{CategoryID: filter.CategoryID, IsActive: filter.IsActive, Search: search})
	if err != nil {
		return nil, 0, fmt.Errorf("count services: %w", err)
	}
	rows, err := q.ListServices(ctx, gen.ListServicesParams{
		CategoryID: filter.CategoryID, IsActive: filter.IsActive, Search: search,
		PageLimit: page.Limit(), PageOffset: page.Offset(),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list services: %w", err)
	}
	return toServices(rows), total, nil
}

func (r *CatalogRepository) ListOfferedServices(ctx context.Context) ([]*entities.Service, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListActiveServicesOfActiveCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list offered services: %w", err)
	}
	return toServices(rows), nil
}

func (r *CatalogRepository) FindService(ctx context.Context, id uuid.UUID) (*entities.Service, error) {
	row, err := postgres.Queries(ctx, r.pool).GetService(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	return toService(row), nil
}

func (r *CatalogRepository) CreateService(ctx context.Context, s *entities.Service) error {
	err := postgres.Queries(ctx, r.pool).CreateService(ctx, gen.CreateServiceParams{
		ID:                  s.ID,
		CategoryID:          s.CategoryID,
		Slug:                s.Slug,
		Name:                s.Name,
		Description:         s.Description,
		Icon:                s.Icon,
		RequiresDestination: s.RequiresDestination,
		BaseFeeCents:        s.Pricing.BaseFeeCents,
		PerKmFeeCents:       s.Pricing.PerKmFeeCents,
		PerMinuteFeeCents:   s.Pricing.PerMinuteFeeCents,
		DisplayOrder:        int32(s.DisplayOrder), // #nosec G115 -- bounded by the entity
		IsActive:            s.IsActive,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	})
	return catalogWriteError("insert service", err)
}

func (r *CatalogRepository) UpdateService(ctx context.Context, s *entities.Service) error {
	err := postgres.Queries(ctx, r.pool).UpdateService(ctx, gen.UpdateServiceParams{
		ID:                  s.ID,
		CategoryID:          s.CategoryID,
		Name:                s.Name,
		Description:         s.Description,
		Icon:                s.Icon,
		RequiresDestination: s.RequiresDestination,
		BaseFeeCents:        s.Pricing.BaseFeeCents,
		PerKmFeeCents:       s.Pricing.PerKmFeeCents,
		PerMinuteFeeCents:   s.Pricing.PerMinuteFeeCents,
		DisplayOrder:        int32(s.DisplayOrder), // #nosec G115 -- bounded by the entity
		IsActive:            s.IsActive,
		UpdatedAt:           s.UpdatedAt,
	})
	return catalogWriteError("update service", err)
}

var catalogConflicts = map[string]error{
	"service_categories_slug_key": repository.ErrCategorySlugTaken,
	"services_slug_key":           repository.ErrServiceSlugTaken,
}

func catalogWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == uniqueViolation {
			if conflict, ok := catalogConflicts[pgErr.ConstraintName]; ok {
				return conflict
			}
		}
		if pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == "services_category_id_fkey" {
			return repository.ErrNotFound
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func toServiceCategory(row gen.ServiceCategory) *entities.ServiceCategory {
	return &entities.ServiceCategory{
		ID:   row.ID,
		Slug: row.Slug,
		CatalogDetails: entities.CatalogDetails{
			Name:         row.Name,
			Description:  row.Description,
			Icon:         row.Icon,
			DisplayOrder: int(row.DisplayOrder),
		},
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func toServices(rows []gen.Service) []*entities.Service {
	services := make([]*entities.Service, 0, len(rows))
	for i := range rows {
		services = append(services, toService(rows[i]))
	}
	return services
}

func toService(row gen.Service) *entities.Service {
	return &entities.Service{
		ID:         row.ID,
		CategoryID: row.CategoryID,
		Slug:       row.Slug,
		CatalogDetails: entities.CatalogDetails{
			Name:         row.Name,
			Description:  row.Description,
			Icon:         row.Icon,
			DisplayOrder: int(row.DisplayOrder),
		},
		RequiresDestination: row.RequiresDestination,
		Pricing: entities.ServicePricing{
			BaseFeeCents:      row.BaseFeeCents,
			PerKmFeeCents:     row.PerKmFeeCents,
			PerMinuteFeeCents: row.PerMinuteFeeCents,
		},
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}
