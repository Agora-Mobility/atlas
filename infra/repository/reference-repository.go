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
)

type ReferenceRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IReferenceRepository = (*ReferenceRepository)(nil)

func NewReferenceRepository(pool *pgxpool.Pool) *ReferenceRepository {
	return &ReferenceRepository{pool: pool}
}

func toCatalogItem(row *gen.CatalogItem, slug string) entities.CatalogItem {
	return entities.CatalogItem{
		ID: row.ID, Kind: entities.CatalogKind(row.Kind), CategoryID: row.CategoryID, CategorySlug: slug, Code: row.Code, Label: row.Label, Description: row.Description,
		AmountCents: row.AmountCents, PromoCents: row.PromoAmountCents, PromoUntil: row.PromoUntil, Attrs: row.Attrs, DisplayOrder: int(row.DisplayOrder), IsActive: row.IsActive,
	}
}

func (r *ReferenceRepository) Items(ctx context.Context, kind entities.CatalogKind, category string, includeInactive bool) ([]entities.CatalogItem, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListCatalogItems(ctx, gen.ListCatalogItemsParams{Kind: string(kind), Category: category, IncludeInactive: includeInactive})
	if err != nil {
		return nil, fmt.Errorf("list catalog items: %w", err)
	}
	out := make([]entities.CatalogItem, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, toCatalogItem(&gen.CatalogItem{ID: row.ID, Kind: row.Kind, CategoryID: row.CategoryID, Code: row.Code, Label: row.Label, Description: row.Description, AmountCents: row.AmountCents, PromoAmountCents: row.PromoAmountCents, PromoUntil: row.PromoUntil, Attrs: row.Attrs, DisplayOrder: row.DisplayOrder, IsActive: row.IsActive}, row.CategorySlug))
	}
	return out, nil
}

func (r *ReferenceRepository) Item(ctx context.Context, id uuid.UUID) (*entities.CatalogItem, error) {
	row, err := postgres.Queries(ctx, r.pool).GetCatalogItem(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get catalog item: %w", err)
	}
	item := toCatalogItem(&gen.CatalogItem{ID: row.ID, Kind: row.Kind, CategoryID: row.CategoryID, Code: row.Code, Label: row.Label, Description: row.Description, AmountCents: row.AmountCents, PromoAmountCents: row.PromoAmountCents, PromoUntil: row.PromoUntil, Attrs: row.Attrs, DisplayOrder: row.DisplayOrder, IsActive: row.IsActive}, row.CategorySlug)
	return &item, nil
}

func (r *ReferenceRepository) ItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]entities.CatalogItem, error) {
	rows, err := postgres.Queries(ctx, r.pool).GetCatalogItems(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get catalog items: %w", err)
	}
	out := make([]entities.CatalogItem, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		out = append(out, toCatalogItem(&gen.CatalogItem{ID: row.ID, Kind: row.Kind, CategoryID: row.CategoryID, Code: row.Code, Label: row.Label, Description: row.Description, AmountCents: row.AmountCents, PromoAmountCents: row.PromoAmountCents, PromoUntil: row.PromoUntil, Attrs: row.Attrs, DisplayOrder: row.DisplayOrder, IsActive: row.IsActive}, row.CategorySlug))
	}
	return out, nil
}

func (r *ReferenceRepository) CreateItem(ctx context.Context, item *entities.CatalogItem) error {
	err := postgres.Queries(ctx, r.pool).InsertCatalogItem(ctx, gen.InsertCatalogItemParams{
		ID: item.ID, Kind: string(item.Kind), CategoryID: item.CategoryID, Code: item.Code, Label: item.Label, Description: item.Description, AmountCents: item.AmountCents,
		PromoAmountCents: item.PromoCents, PromoUntil: item.PromoUntil, Attrs: item.Attrs, DisplayOrder: int32(item.DisplayOrder), IsActive: item.IsActive, // #nosec G115 -- validated by the entity
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return repository.ErrCatalogCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create catalog item: %w", err)
	}
	return nil
}

func (r *ReferenceRepository) UpdateItem(ctx context.Context, item *entities.CatalogItem) error {
	err := postgres.Queries(ctx, r.pool).UpdateCatalogItem(ctx, gen.UpdateCatalogItemParams{
		ID: item.ID, Label: item.Label, Description: item.Description, AmountCents: item.AmountCents, PromoAmountCents: item.PromoCents, PromoUntil: item.PromoUntil, Attrs: item.Attrs,
		DisplayOrder: int32(item.DisplayOrder), IsActive: item.IsActive, // #nosec G115 -- validated by the entity
	})
	if err != nil {
		return fmt.Errorf("update catalog item: %w", err)
	}
	return nil
}

func (r *ReferenceRepository) Setting(ctx context.Context, key string) (string, error) {
	value, err := postgres.Queries(ctx, r.pool).GetPlatformSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting: %w", err)
	}
	return value, nil
}

func (r *ReferenceRepository) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListPlatformSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	out := make(map[string]string, len(rows))
	for i := range rows {
		out[rows[i].Key] = rows[i].Value
	}
	return out, nil
}

func (r *ReferenceRepository) SetSetting(ctx context.Context, key, value string) error {
	if err := postgres.Queries(ctx, r.pool).UpsertPlatformSetting(ctx, gen.UpsertPlatformSettingParams{Key: key, Value: value}); err != nil {
		return fmt.Errorf("set setting: %w", err)
	}
	return nil
}
