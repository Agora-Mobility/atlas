package h3index_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
)

type city struct {
	name     string
	lat, lng float64
}

var africanCities = []city{
	{"Luanda", -8.8390, 13.2894}, {"Huambo", -12.7761, 15.7392}, {"Saurimo", -9.6608, 20.3916},
	{"Benguela", -12.5763, 13.4055}, {"Lubango", -14.9177, 13.4925}, {"Kinshasa", -4.4419, 15.2663},
	{"Lagos", 6.5244, 3.3792}, {"Abidjan", 5.3600, -4.0083}, {"Accra", 5.6037, -0.1870},
	{"Dakar", 14.7167, -17.4677}, {"Nairobi", -1.2921, 36.8219}, {"Kampala", 0.3476, 32.5825},
	{"Dar es Salaam", -6.7924, 39.2083}, {"Maputo", -25.9692, 32.5732}, {"Johannesburg", -26.2041, 28.0473},
	{"Addis Ababa", 9.0300, 38.7400}, {"Cairo", 30.0444, 31.2357},
}

const (
	originsPerCity  = 60
	bearingStep     = 3.0
	originSpreadDeg = 0.25
	earthRadiusKm   = 6371.0088
)

func TestRingsMatchTheProposalTable(t *testing.T) {
	index := h3index.New()
	cases := []struct {
		resolution int
		radiusKm   float64
		rings      int
		cells      int
	}{
		{7, 3, 3, 37}, {7, 5, 5, 91}, {7, 10, 8, 217},
		{5, 10, 2, 19}, {5, 30, 4, 61}, {5, 80, 9, 271},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("r%d_%.0fkm", c.resolution, c.radiusKm), func(t *testing.T) {
			rings := index.RingsFor(c.radiusKm, c.resolution)
			assert.Equal(t, c.rings, rings)
			assert.Equal(t, c.cells, index.CellCountFor(rings))
		})
	}
}

func TestRingsCoverEveryProviderInsideTheRadiusAcrossAfricanCities(t *testing.T) {
	index := h3index.New()
	searches := []struct {
		resolution int
		radiusKm   float64
	}{
		{7, 3}, {7, 5}, {7, 10}, {5, 10}, {5, 30}, {5, 80}, {6, 30}, {6, 50},
	}
	for _, search := range searches {
		for _, c := range africanCities {
			t.Run(fmt.Sprintf("%s_r%d_%.0fkm", c.name, search.resolution, search.radiusKm), func(t *testing.T) {
				for _, origin := range originsAround(c) {
					disk, err := index.Covering(origin, search.radiusKm, search.resolution)
					require.NoError(t, err)
					inDisk := make(map[int64]struct{}, len(disk))
					for _, cell := range disk {
						inDisk[cell] = struct{}{}
					}
					for bearing := 0.0; bearing < 360; bearing += bearingStep {
						edge := destination(origin, bearing, search.radiusKm)
						cell, err := index.Cell(edge, search.resolution)
						require.NoError(t, err)
						_, covered := inDisk[cell]
						require.True(t, covered, "origin %+v bearing %.0f escapes k=%d", origin, bearing, index.RingsFor(search.radiusKm, search.resolution))
					}
				}
			})
		}
	}
}

func TestResolutionsThatAreNotMeasuredStillGiveASafeRingCount(t *testing.T) {
	index := h3index.New()
	assert.Equal(t, 1, index.RingsFor(0, 7))
	assert.Greater(t, index.RingsFor(10, 9), index.RingsFor(10, 8))
}

func TestCellFitsInAPositiveBigint(t *testing.T) {
	index := h3index.New()
	for _, c := range africanCities {
		for _, resolution := range []int{5, 6, 7, 8} {
			cell, err := index.Cell(entities.GeoPoint{Lat: c.lat, Lng: c.lng}, resolution)
			require.NoError(t, err)
			assert.Positive(t, cell)
		}
	}
}

func TestInteriorCellsExcludeTheBorder(t *testing.T) {
	index := h3index.New()
	square := entities.GeoPolygon{Outer: []entities.GeoPoint{
		{Lat: -9.2, Lng: 13.0}, {Lat: -9.2, Lng: 13.6}, {Lat: -8.6, Lng: 13.6}, {Lat: -8.6, Lng: 13.0}, {Lat: -9.2, Lng: 13.0},
	}}

	cells, err := index.InteriorCells([]entities.GeoPolygon{square}, h3index.Coverage)
	require.NoError(t, err)
	require.NotEmpty(t, cells)

	centerCell, err := index.Cell(entities.GeoPoint{Lat: -8.9, Lng: 13.3}, h3index.Coverage)
	require.NoError(t, err)
	assert.Contains(t, cells, centerCell)

	nearBorder, err := index.Cell(entities.GeoPoint{Lat: -8.601, Lng: 13.3}, h3index.Coverage)
	require.NoError(t, err)
	assert.NotContains(t, cells, nearBorder)
}

func TestCenterAndFormat(t *testing.T) {
	index := h3index.New()
	point := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	cell, err := index.Cell(point, h3index.Analytics)
	require.NoError(t, err)

	center, err := index.Center(cell)
	require.NoError(t, err)
	assert.Less(t, entities.DistanceMeters(point, center), 1500.0)
	assert.Len(t, index.Format(cell), 15)
}

func BenchmarkCell(b *testing.B) {
	index := h3index.New()
	point := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	for b.Loop() {
		_, _ = index.Cell(point, h3index.LiveUrban)
	}
}

func BenchmarkDiskK8(b *testing.B) {
	index := h3index.New()
	cell, _ := index.Cell(entities.GeoPoint{Lat: -8.84, Lng: 13.23}, h3index.LiveUrban)
	for b.Loop() {
		_, _ = index.Disk(cell, 8)
	}
}

func originsAround(c city) []entities.GeoPoint {
	origins := make([]entities.GeoPoint, 0, originsPerCity)
	side := int(math.Ceil(math.Sqrt(originsPerCity)))
	for i := range originsPerCity {
		row, col := float64(i/side), float64(i%side)
		origins = append(origins, entities.GeoPoint{
			Lat: c.lat + originSpreadDeg*(row/float64(side)-0.5),
			Lng: c.lng + originSpreadDeg*(col/float64(side)-0.5),
		})
	}
	return origins
}

func destination(origin entities.GeoPoint, bearingDeg, distanceKm float64) entities.GeoPoint {
	angular := distanceKm / earthRadiusKm
	bearing := bearingDeg * math.Pi / 180
	lat1 := origin.Lat * math.Pi / 180
	lng1 := origin.Lng * math.Pi / 180
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(angular) + math.Cos(lat1)*math.Sin(angular)*math.Cos(bearing))
	lng2 := lng1 + math.Atan2(math.Sin(bearing)*math.Sin(angular)*math.Cos(lat1), math.Cos(angular)-math.Sin(lat1)*math.Sin(lat2))
	return entities.GeoPoint{Lat: lat2 * 180 / math.Pi, Lng: lng2 * 180 / math.Pi}
}

func TestBoundaryWrapsTheCell(t *testing.T) {
	index := h3index.New()
	luanda := entities.GeoPoint{Lat: -8.8390, Lng: 13.2894}
	cell, err := index.Cell(luanda, h3index.Analytics)
	require.NoError(t, err)

	boundary, err := index.Boundary(cell)
	require.NoError(t, err)

	require.Len(t, boundary, 6)
	for _, vertex := range boundary {
		assert.Less(t, entities.DistanceMeters(luanda, vertex), 1500.0, "every vertex sits next to the point")
	}
}
