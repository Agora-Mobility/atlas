package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type DemandRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IDemandRepository = (*DemandRepository)(nil)

func NewDemandRepository(pool *pgxpool.Pool) *DemandRepository {
	return &DemandRepository{pool: pool}
}

func (r *DemandRepository) ListDemandPricing(ctx context.Context) ([]*entities.DemandPricing, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDemandPricing(ctx)
	if err != nil {
		return nil, fmt.Errorf("list demand pricing: %w", err)
	}
	out := make([]*entities.DemandPricing, 0, len(rows))
	for i := range rows {
		pricing, err := toDemandPricing(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, pricing)
	}
	return out, nil
}

func (r *DemandRepository) FindDemandPricing(ctx context.Context, zoneID, categoryID uuid.UUID) (*entities.DemandPricing, error) {
	row, err := postgres.Queries(ctx, r.pool).GetDemandPricing(ctx, gen.GetDemandPricingParams{ZoneID: zoneID, CategoryID: categoryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get demand pricing: %w", err)
	}
	return toDemandPricing(&row)
}

func (r *DemandRepository) SaveDemandPricing(ctx context.Context, d *entities.DemandPricing) error {
	tiers, err := json.Marshal(d.Tiers)
	if err != nil {
		return fmt.Errorf("encode tiers: %w", err)
	}
	err = postgres.Queries(ctx, r.pool).UpsertDemandPricing(ctx, gen.UpsertDemandPricingParams{
		ID: d.ID, ZoneID: d.ZoneID, CategoryID: d.CategoryID, Mode: string(d.Mode), Tiers: tiers, MaxFactorBps: int32(d.MaxFactorBps), // #nosec G115 -- validated by the entity
		CommissionOnSurge: d.CommissionOnSurge, UpdatedBy: d.UpdatedBy, UpdatedAt: d.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("save demand pricing: %w", err)
	}
	return nil
}

func (r *DemandRepository) PendingCustomers(ctx context.Context, zoneID, categoryID uuid.UUID) (map[int64][]uuid.UUID, error) {
	rows, err := postgres.Queries(ctx, r.pool).PendingCustomersByCell(ctx, gen.PendingCustomersByCellParams{ZoneID: zoneID, CategoryID: categoryID})
	if err != nil {
		return nil, fmt.Errorf("pending customers by cell: %w", err)
	}
	out := make(map[int64][]uuid.UUID, len(rows))
	for _, row := range rows {
		out[row.PickupH3R8] = append(out[row.PickupH3R8], row.CustomerID)
	}
	return out, nil
}

func (r *DemandRepository) SaveSnapshots(ctx context.Context, snapshots []entities.DemandSnapshot) error {
	queries := postgres.Queries(ctx, r.pool)
	for i := range snapshots {
		s := &snapshots[i]
		err := queries.InsertDemandSnapshot(ctx, gen.InsertDemandSnapshotParams{
			ZoneID: s.ZoneID, CategoryID: s.CategoryID, AreaH3: s.Area, Pending: int32(s.Pending), Free: int32(s.Free), // #nosec G115 -- counts of requests and providers
			RatioBps: int32(s.RatioBps), FactorBps: int32(s.FactorBps), RecordedAt: s.RecordedAt, // #nosec G115 -- bounded ratios
		})
		if err != nil {
			return fmt.Errorf("save demand snapshot: %w", err)
		}
	}
	return nil
}

func (r *DemandRepository) ListSnapshots(ctx context.Context, zoneID, categoryID uuid.UUID, since time.Time, limit int) ([]entities.DemandSnapshot, error) {
	rows, err := postgres.Queries(ctx, r.pool).ListDemandSnapshots(ctx, gen.ListDemandSnapshotsParams{
		ZoneID: zoneID, CategoryID: categoryID, Since: since, RowLimit: int32(limit), // #nosec G115 -- bounded by the caller
	})
	if err != nil {
		return nil, fmt.Errorf("list demand snapshots: %w", err)
	}
	out := make([]entities.DemandSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.DemandSnapshot{ZoneID: row.ZoneID, CategoryID: row.CategoryID, DemandArea: entities.DemandArea{
			Area: row.AreaH3, Pending: int(row.Pending), Free: int(row.Free), RatioBps: int(row.RatioBps), FactorBps: int(row.FactorBps), RecordedAt: row.RecordedAt,
		}})
	}
	return out, nil
}

func (r *DemandRepository) PurgeSnapshots(ctx context.Context, before time.Time) (int64, error) {
	removed, err := postgres.Queries(ctx, r.pool).PurgeDemandSnapshots(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("purge demand snapshots: %w", err)
	}
	return removed, nil
}

func toDemandPricing(row *gen.DemandPricing) (*entities.DemandPricing, error) {
	pricing := &entities.DemandPricing{
		ID: row.ID, ZoneID: row.ZoneID, CategoryID: row.CategoryID, Mode: entities.DemandMode(row.Mode), MaxFactorBps: int(row.MaxFactorBps),
		CommissionOnSurge: row.CommissionOnSurge, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal(row.Tiers, &pricing.Tiers); err != nil {
		return nil, fmt.Errorf("decode demand tiers: %w", err)
	}
	return pricing, nil
}
