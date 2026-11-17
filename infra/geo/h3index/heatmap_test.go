package h3index_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

var luandaCenter = entities.GeoPoint{Lat: -8.8390, Lng: 13.2894}

func TestTheLuandaPointHasAStableCell(t *testing.T) {
	index := h3index.New()

	cell, err := index.Cell(luandaCenter, entities.HeatmapResolution)
	require.NoError(t, err)
	assert.Equal(t, "89831e395d3ffff", index.Format(cell))
	assert.EqualValues(t, 619299802304479231, cell)

	parent, err := index.Parent(cell, 8)
	require.NoError(t, err)
	assert.Equal(t, "88831e395dfffff", index.Format(parent))
}

func TestBoxCellsFillsTheMapView(t *testing.T) {
	index := h3index.New()
	box := entities.GeoBox{MinLng: 13.15, MinLat: -8.95, MaxLng: 13.35, MaxLat: -8.75}

	cells, err := index.BoxCells(box, 8, 20_000)
	require.NoError(t, err)
	assert.Len(t, cells, 618)
	center, err := index.Cell(luandaCenter, 8)
	require.NoError(t, err)
	assert.Contains(t, cells, center)
	for _, cell := range cells {
		point, err := index.Center(cell)
		require.NoError(t, err)
		assert.True(t, point.Lat >= box.MinLat && point.Lat <= box.MaxLat && point.Lng >= box.MinLng && point.Lng <= box.MaxLng)
	}
}

func TestBoxCellsRefusesMoreCellsThanTheLimit(t *testing.T) {
	index := h3index.New()
	luanda := entities.GeoBox{MinLng: 13.15, MinLat: -8.95, MaxLng: 13.35, MaxLat: -8.75}
	angola := entities.GeoBox{MinLng: 11, MinLat: -18, MaxLng: 24, MaxLat: -4}

	_, err := index.BoxCells(luanda, 8, 600)
	require.ErrorIs(t, err, services.ErrTooManyCells, "counted after the fill")
	_, err = index.BoxCells(angola, 9, 20_000)
	require.ErrorIs(t, err, services.ErrTooManyCells, "refused from the area, before allocating millions of cells")
	cells, err := index.BoxCells(angola, 5, 20_000)
	require.NoError(t, err)
	assert.NotEmpty(t, cells)
}
