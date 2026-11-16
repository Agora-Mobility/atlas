package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

var (
	heatNow    = time.Date(2026, 11, 12, 10, 7, 0, 0, time.UTC)
	luandaView = entities.GeoBox{MinLng: 13.15, MinLat: -8.95, MaxLng: 13.35, MaxLat: -8.75}
)

type heatStore struct {
	repository.IHeatmapRepository
	sums  []repository.HeatmapSum
	asked repository.HeatmapRange
	calls int
}

func (s *heatStore) Sum(_ context.Context, query repository.HeatmapRange) ([]repository.HeatmapSum, error) {
	s.asked, s.calls = query, s.calls+1
	return s.sums, nil
}

type memoryCache struct{ values map[string]dtos.HeatCellsView }

func (c *memoryCache) Get(_ context.Context, key string, dest any) (bool, error) {
	value, ok := c.values[key]
	if ok {
		*dest.(*dtos.HeatCellsView) = value
	}
	return ok, nil
}

func (c *memoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.values[key] = *value.(*dtos.HeatCellsView)
	return nil
}

func heatSettings() HeatmapSettings {
	return HeatmapSettings{
		Interval: 5 * time.Minute, Window: 15 * time.Minute, LookbackWindows: 2, Rollups: []int{8, 7, 6},
		SelfWeight: 0.6, NeighborWeight: 0.4, MaxCells: 20_000, MaxPeriod: 7 * 24 * time.Hour, CacheTTL: time.Minute,
		Zooms: []ZoomStep{{MinZoom: 0, Resolution: 6}, {MinZoom: 11, Resolution: 7}, {MinZoom: 13, Resolution: 8}, {MinZoom: 15, Resolution: 9}},
	}
}

func heatUsecase(store *heatStore, settings HeatmapSettings) *Usecase {
	u := NewUsecase(Deps{Geo: h3index.New(), Heatmap: store, Cache: &memoryCache{values: map[string]dtos.HeatCellsView{}}}, Settings{Heatmap: settings})
	u.now = func() time.Time { return heatNow }
	return u
}

func luandaCell(t *testing.T, resolution int) int64 {
	t.Helper()
	cell, err := h3index.New().Cell(entities.GeoPoint{Lat: -8.8390, Lng: 13.2894}, resolution)
	require.NoError(t, err)
	return cell
}

func TestRollUpSumsTheChildrenIntoTheirParent(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())
	geo := h3index.New()
	parent, err := geo.Parent(luandaCell(t, 9), 8)
	require.NoError(t, err)
	children, err := geo.Children(parent, 9)
	require.NoError(t, err)
	around, err := geo.Disk(parent, 1)
	require.NoError(t, err)
	otherParent := around[0]
	if otherParent == parent {
		otherParent = around[1]
	}
	cousins, err := geo.Children(otherParent, 9)
	require.NoError(t, err)
	elsewhere := cousins[0]

	parents, err := u.rollUp(map[int64]int64{children[0]: 2, children[1]: 3, children[2]: 5, elsewhere: 4}, 8)

	require.NoError(t, err)
	assert.Equal(t, map[int64]int64{parent: 10, otherParent: 4}, parents)
}

func TestSmoothingMixesTheCellWithTheMeanOfItsNeighbors(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())
	geo := h3index.New()
	busy := luandaCell(t, 9)
	ring, err := geo.Disk(busy, 1)
	require.NoError(t, err)
	var near int64
	for _, cell := range ring {
		if cell != busy {
			near = cell
			break
		}
	}

	smoothed, err := u.smooth(map[int64]int64{busy: 10, near: 4})

	require.NoError(t, err)
	assert.Len(t, smoothed, 10, "both cells and the eight cells around them")
	assert.InDelta(t, 0.6*10+0.4*4.0/6, smoothed[busy].value, 1e-9)
	assert.InDelta(t, 0.6*4+0.4*10.0/6, smoothed[near].value, 1e-9)
	assert.EqualValues(t, 10, smoothed[busy].count)
	nearRing, err := geo.Disk(near, 1)
	require.NoError(t, err)
	for _, cell := range ring {
		if cell == busy || cell == near {
			continue
		}
		expected := 0.4 * 10.0 / 6
		if contains(nearRing, cell) {
			expected = 0.4 * 14.0 / 6
		}
		assert.InDelta(t, expected, smoothed[cell].value, 1e-9)
		assert.Zero(t, smoothed[cell].count, "a neighbor without requests still counts none")
	}
}

func contains(cells []int64, cell int64) bool {
	for _, c := range cells {
		if c == cell {
			return true
		}
	}
	return false
}

func TestHeatLayersKeepTheCountAtEveryResolution(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())

	layers, err := u.heatLayers(map[int64]int64{luandaCell(t, 9): 7})

	require.NoError(t, err)
	require.Len(t, layers, 4)
	for _, resolution := range []int{9, 8, 7, 6} {
		var total int64
		for _, heat := range layers[resolution] {
			total += heat.count
		}
		assert.EqualValues(t, 7, total, "resolution %d", resolution)
		assert.InDelta(t, 0.6*7, layers[resolution][luandaCell(t, resolution)].value, 1e-9)
	}
}

func TestZoomPicksTheResolution(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())
	for zoom, resolution := range map[int]int{0: 6, 10: 6, 11: 7, 12: 7, 13: 8, 14: 8, 15: 9, 22: 9} {
		assert.Equal(t, resolution, u.zoomResolution(zoom), "zoom %d", zoom)
	}
}

func TestHeatCellsAnswersInHexadecimal(t *testing.T) {
	store := &heatStore{sums: []repository.HeatmapSum{{Cell: luandaCell(t, 8), Count: 3, Value: 2.5}, {Cell: luandaCell(t, 8) + 1, Count: 0, Value: 0}}}
	u := heatUsecase(store, heatSettings())

	view, err := u.HeatCells(context.Background(), dtos.HeatCellsQuery{Box: luandaView, Zoom: 13})

	require.NoError(t, err)
	assert.Equal(t, 8, view.Resolution)
	assert.Equal(t, MetricValue, view.Metric)
	assert.Equal(t, dtos.HeatWindow{From: time.Date(2026, 11, 12, 9, 45, 0, 0, time.UTC), To: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)}, view.Window, "the last complete window")
	assert.Equal(t, []dtos.HeatValue{{H3: "88831e395dfffff", Value: 2.5}}, view.Cells, "empty cells are left out")
	assert.InDelta(t, 2.5, view.Max, 1e-9)
	assert.Len(t, store.asked.Cells, 618)

	counted, err := u.HeatCells(context.Background(), dtos.HeatCellsQuery{Box: luandaView, Zoom: 13, Metric: MetricCount})
	require.NoError(t, err)
	assert.Equal(t, []dtos.HeatValue{{H3: "88831e395dfffff", Value: 3}}, counted.Cells)
}

func TestHeatCellsComesFromTheCacheTheSecondTime(t *testing.T) {
	store := &heatStore{sums: []repository.HeatmapSum{{Cell: luandaCell(t, 8), Count: 3, Value: 2.5}}}
	u := heatUsecase(store, heatSettings())
	query := dtos.HeatCellsQuery{Box: luandaView, Zoom: 13}
	query.Box.MaxLat = -8.7499

	first, err := u.HeatCells(context.Background(), query)
	require.NoError(t, err)
	query.Box.MaxLat = -8.7493
	second, err := u.HeatCells(context.Background(), query)
	require.NoError(t, err)

	assert.Equal(t, 1, store.calls, "a box that rounds to the same view hits the cache")
	assert.Equal(t, first, second)
}

func TestHeatCellsGoesCoarserWhenTheViewHoldsTooManyCells(t *testing.T) {
	settings := heatSettings()
	settings.MaxCells = 100
	store := &heatStore{}
	u := heatUsecase(store, settings)

	view, err := u.HeatCells(context.Background(), dtos.HeatCellsQuery{Box: luandaView, Zoom: 15})
	require.NoError(t, err)
	assert.Equal(t, 7, view.Resolution, "618 cells at 8 and about 3500 at 9 do not fit in 100")
	assert.Equal(t, 7, store.asked.Resolution)

	settings.MaxCells = 1
	_, err = heatUsecase(store, settings).HeatCells(context.Background(), dtos.HeatCellsQuery{Box: luandaView, Zoom: 15})
	assert.ErrorIs(t, err, ErrHeatmapArea)
}

func TestHeatCellsValidatesTheQuery(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())
	ten := heatNow.Add(-10 * 24 * time.Hour)
	later := heatNow.Add(time.Hour)
	cases := map[string]struct {
		query dtos.HeatCellsQuery
		err   error
	}{
		"latitude out of range":  {dtos.HeatCellsQuery{Box: entities.GeoBox{MinLng: 13, MinLat: -91, MaxLng: 14, MaxLat: -8}, Zoom: 12}, ErrHeatmapBox},
		"longitude out of range": {dtos.HeatCellsQuery{Box: entities.GeoBox{MinLng: -181, MinLat: -9, MaxLng: 14, MaxLat: -8}, Zoom: 12}, ErrHeatmapBox},
		"minimum above maximum":  {dtos.HeatCellsQuery{Box: entities.GeoBox{MinLng: 14, MinLat: -9, MaxLng: 13, MaxLat: -8}, Zoom: 12}, ErrHeatmapBox},
		"zoom out of range":      {dtos.HeatCellsQuery{Box: luandaView, Zoom: 25}, ErrHeatmapZoom},
		"unknown metric":         {dtos.HeatCellsQuery{Box: luandaView, Zoom: 12, Metric: "sum"}, ErrHeatmapMetric},
		"more than seven days":   {dtos.HeatCellsQuery{Box: luandaView, Zoom: 12, From: &ten}, ErrHeatmapPeriod},
		"ends before it starts":  {dtos.HeatCellsQuery{Box: luandaView, Zoom: 12, From: &later, To: &ten}, ErrHeatmapPeriod},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := u.HeatCells(context.Background(), tc.query)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}

func TestHeatPeriodAlignsToWholeWindows(t *testing.T) {
	u := heatUsecase(&heatStore{}, heatSettings())
	from := time.Date(2026, 11, 11, 8, 20, 0, 0, time.UTC)
	to := time.Date(2026, 11, 11, 9, 5, 0, 0, time.UTC)

	start, end, err := u.heatPeriod(&from, &to)

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 11, 8, 15, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 11, 11, 9, 15, 0, 0, time.UTC), end)
}
