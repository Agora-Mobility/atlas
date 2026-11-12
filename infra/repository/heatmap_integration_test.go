//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

type storedHeatCell struct {
	WindowStart time.Time
	Resolution  int
	Category    string
	Cell        int64
	Count       int64
	Value       float64
}

func heatmapUsecase(app *pgxpool.Pool) *dispatch.Usecase {
	return dispatch.NewUsecase(dispatch.Deps{
		Heatmap: repository.NewHeatmapRepository(app), Geo: h3index.New(), Tx: postgres.NewTxManager(app),
	}, dispatch.Settings{Heatmap: dispatch.HeatmapSettings{
		Interval: time.Minute, Window: 15 * time.Minute, LookbackWindows: 2, Rollups: []int{8, 7, 6},
		SelfWeight: 0.6, NeighborWeight: 0.4, Zooms: []dispatch.ZoomStep{{MinZoom: 0, Resolution: 6}}, MaxCells: 20_000, MaxPeriod: 7 * 24 * time.Hour,
	}})
}

func storedHeatCells(t *testing.T, owner *pgxpool.Pool) []storedHeatCell {
	t.Helper()
	rows, err := owner.Query(context.Background(), `SELECT window_start, resolution, category_slug, h3, count, value FROM heatmap_cells ORDER BY window_start, resolution, category_slug, h3`)
	require.NoError(t, err)
	defer rows.Close()
	var cells []storedHeatCell
	for rows.Next() {
		var cell storedHeatCell
		require.NoError(t, rows.Scan(&cell.WindowStart, &cell.Resolution, &cell.Category, &cell.Cell, &cell.Count, &cell.Value))
		cell.WindowStart = cell.WindowStart.UTC()
		cells = append(cells, cell)
	}
	require.NoError(t, rows.Err())
	return cells
}

func TestHeatmapAggregationIsIdempotent(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	uc := heatmapUsecase(app)
	const requests = 3
	for range requests {
		request, _ := seedRequest(t, app, owner)
		_, err := owner.Exec(ctx, `UPDATE service_requests SET public_code = id::text WHERE id = $1`, request.ID)
		require.NoError(t, err)
	}
	pickupCell, err := h3index.New().Cell(entities.GeoPoint{Lat: -8.84, Lng: 13.23}, entities.HeatmapResolution)
	require.NoError(t, err)

	_, err = owner.Exec(ctx, `UPDATE service_requests SET pickup_h3_r9 = $1`, pickupCell)
	require.NoError(t, err)

	now := time.Now().UTC()
	from, to := now.Add(-time.Hour), now.Add(15*time.Minute)
	require.NoError(t, uc.AggregateHeatmapBetween(ctx, from, to))
	first := storedHeatCells(t, owner)
	require.NoError(t, uc.AggregateHeatmapBetween(ctx, from, to))
	assert.Equal(t, first, storedHeatCells(t, owner), "running twice neither duplicates nor adds up")

	byResolution := map[int][]storedHeatCell{}
	for _, cell := range first {
		byResolution[cell.Resolution] = append(byResolution[cell.Resolution], cell)
	}
	require.Len(t, byResolution, 4)
	for resolution, cells := range byResolution {
		assert.Len(t, cells, 7, "the busy cell and its six neighbors at resolution %d", resolution)
	}
	window := now.Truncate(15 * time.Minute)
	for _, cell := range byResolution[entities.HeatmapResolution] {
		if cell.Cell == pickupCell {
			assert.Equal(t, window, cell.WindowStart)
			assert.EqualValues(t, requests, cell.Count)
			assert.InDelta(t, 0.6*requests, cell.Value, 1e-9)
		}
	}
	_, err = owner.Exec(ctx, `INSERT INTO heatmap_cells (window_start, resolution, category_slug, h3, count, value, updated_at) VALUES ($1, 9, 'taxi', 1, 50, 50, $2)`, window, now.Add(-time.Hour))
	require.NoError(t, err)
	require.NoError(t, uc.AggregateHeatmapBetween(ctx, from, to))
	assert.Equal(t, first, storedHeatCells(t, owner), "a cell no longer in the window is removed")

	removed, err := repository.NewHeatmapRepository(app).Purge(ctx, to.Add(time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, len(first), removed)
}

func TestHeatmapAggregationRunsOnOneInstanceAtATime(t *testing.T) {
	app, _ := setupDBs(t)
	ctx := context.Background()
	uc := heatmapUsecase(app)
	tx := postgres.NewTxManager(app)

	err := tx.WithinTransaction(ctx, func(ctx context.Context) error {
		acquired, err := repository.NewHeatmapRepository(app).TryLock(ctx)
		require.NoError(t, err)
		require.True(t, acquired)
		now := time.Now().UTC()
		assert.ErrorIs(t, uc.AggregateHeatmapBetween(context.Background(), now.Add(-time.Hour), now), dispatch.ErrHeatmapBusy)
		assert.NoError(t, uc.AggregateHeatmap(context.Background()), "the periodic job skips quietly")
		return nil
	})
	require.NoError(t, err)
}
