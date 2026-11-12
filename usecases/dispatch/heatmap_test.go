package dispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

var heatNow = time.Date(2026, 11, 12, 10, 7, 0, 0, time.UTC)

type heatStore struct {
	repository.IHeatmapRepository
}

func heatSettings() HeatmapSettings {
	return HeatmapSettings{
		Interval: 5 * time.Minute, Window: 15 * time.Minute, LookbackWindows: 2, Rollups: []int{8, 7, 6},
		SelfWeight: 0.6, NeighborWeight: 0.4, MaxCells: 20_000, MaxPeriod: 7 * 24 * time.Hour, CacheTTL: time.Minute,
		Zooms: []ZoomStep{{MinZoom: 0, Resolution: 6}, {MinZoom: 11, Resolution: 7}, {MinZoom: 13, Resolution: 8}, {MinZoom: 15, Resolution: 9}},
	}
}

func heatUsecase(store *heatStore, settings HeatmapSettings) *Usecase {
	u := NewUsecase(Deps{Geo: h3index.New(), Heatmap: store}, Settings{Heatmap: settings})
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
