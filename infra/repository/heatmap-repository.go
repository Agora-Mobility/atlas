package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/database/sqlc/gen"
)

type HeatmapRepository struct {
	pool *pgxpool.Pool
}

var _ repository.IHeatmapRepository = (*HeatmapRepository)(nil)

func NewHeatmapRepository(pool *pgxpool.Pool) *HeatmapRepository {
	return &HeatmapRepository{pool: pool}
}

func (r *HeatmapRepository) TryLock(ctx context.Context) (bool, error) {
	acquired, err := postgres.Queries(ctx, r.pool).TryLockHeatmap(ctx)
	if err != nil {
		return false, fmt.Errorf("lock heatmap: %w", err)
	}
	return acquired, nil
}

func (r *HeatmapRepository) CountRequests(ctx context.Context, from, to time.Time) ([]repository.HeatmapCount, error) {
	rows, err := postgres.Queries(ctx, r.pool).CountRequestsByHeatCell(ctx, gen.CountRequestsByHeatCellParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, fmt.Errorf("count requests by heat cell: %w", err)
	}
	out := make([]repository.HeatmapCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, repository.HeatmapCount{Category: row.CategorySlug, Cell: row.Cell, Requests: row.Requests})
	}
	return out, nil
}

func (r *HeatmapRepository) SaveWindow(ctx context.Context, windowStart time.Time, cells []repository.HeatmapCell, at time.Time) error {
	params := gen.UpsertHeatmapCellsParams{
		WindowStart: windowStart, UpdatedAt: at,
		Resolutions: make([]int16, 0, len(cells)), Categories: make([]string, 0, len(cells)), Cells: make([]int64, 0, len(cells)),
		Counts: make([]int32, 0, len(cells)), HeatValues: make([]float64, 0, len(cells)),
	}
	for _, cell := range cells {
		params.Resolutions = append(params.Resolutions, int16(cell.Resolution)) // #nosec G115 -- H3 resolutions are 0 to 15
		params.Categories = append(params.Categories, cell.Category)
		params.Cells = append(params.Cells, cell.Cell)
		params.Counts = append(params.Counts, int32(cell.Count)) // #nosec G115 -- requests of one cell in one window
		params.HeatValues = append(params.HeatValues, cell.Value)
	}
	queries := postgres.Queries(ctx, r.pool)
	if len(cells) > 0 {
		if err := queries.UpsertHeatmapCells(ctx, params); err != nil {
			return fmt.Errorf("upsert heatmap cells: %w", err)
		}
	}
	if _, err := queries.DeleteStaleHeatmapCells(ctx, gen.DeleteStaleHeatmapCellsParams{WindowStart: windowStart, UpdatedAt: at}); err != nil {
		return fmt.Errorf("delete stale heatmap cells: %w", err)
	}
	return nil
}

func (r *HeatmapRepository) Purge(ctx context.Context, before time.Time) (int64, error) {
	removed, err := postgres.Queries(ctx, r.pool).PurgeHeatmapCells(ctx, before)
	if err != nil {
		return 0, fmt.Errorf("purge heatmap cells: %w", err)
	}
	return removed, nil
}

func (r *HeatmapRepository) Sum(ctx context.Context, query repository.HeatmapRange) ([]repository.HeatmapSum, error) {
	rows, err := postgres.Queries(ctx, r.pool).SumHeatmapCells(ctx, gen.SumHeatmapCellsParams{
		Resolution: int16(query.Resolution), FromAt: query.From, ToAt: query.To, Cells: query.Cells, Category: query.Category, // #nosec G115 -- H3 resolutions are 0 to 15
	})
	if err != nil {
		return nil, fmt.Errorf("sum heatmap cells: %w", err)
	}
	out := make([]repository.HeatmapSum, 0, len(rows))
	for _, row := range rows {
		out = append(out, repository.HeatmapSum{Cell: row.H3, Count: row.Count, Value: row.Value})
	}
	return out, nil
}

func (r *HeatmapRepository) PickupsWithoutCell(ctx context.Context, after uuid.UUID, limit int) ([]repository.RequestPickup, error) {
	rows, err := postgres.Queries(ctx, r.pool).RequestsWithoutHeatCell(ctx, gen.RequestsWithoutHeatCellParams{After: after, Batch: int32(limit)}) // #nosec G115 -- batch size
	if err != nil {
		return nil, fmt.Errorf("requests without heat cell: %w", err)
	}
	out := make([]repository.RequestPickup, 0, len(rows))
	for _, row := range rows {
		out = append(out, repository.RequestPickup{ID: row.ID, Pickup: entities.GeoPoint{Lat: row.Lat, Lng: row.Lng}})
	}
	return out, nil
}

func (r *HeatmapRepository) SetPickupCells(ctx context.Context, cells map[uuid.UUID]int64) (int64, error) {
	params := gen.SetRequestHeatCellsParams{Ids: make([]uuid.UUID, 0, len(cells)), Cells: make([]int64, 0, len(cells))}
	for id, cell := range cells {
		params.Ids = append(params.Ids, id)
		params.Cells = append(params.Cells, cell)
	}
	updated, err := postgres.Queries(ctx, r.pool).SetRequestHeatCells(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("set request heat cells: %w", err)
	}
	return updated, nil
}
