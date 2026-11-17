package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

func TestGeoBoxValidity(t *testing.T) {
	assert.True(t, entities.GeoBox{MinLng: 13.15, MinLat: -8.95, MaxLng: 13.35, MaxLat: -8.75}.IsValid())
	for name, box := range map[string]entities.GeoBox{
		"latitude above 90":      {MinLng: 13, MinLat: -8, MaxLng: 14, MaxLat: 91},
		"longitude below -180":   {MinLng: -181, MinLat: -9, MaxLng: 14, MaxLat: -8},
		"empty":                  {MinLng: 13, MinLat: -9, MaxLng: 13, MaxLat: -8},
		"west east swapped":      {MinLng: 14, MinLat: -9, MaxLng: 13, MaxLat: -8},
		"half the world or more": {MinLng: -90, MinLat: -9, MaxLng: 90, MaxLat: -8},
	} {
		assert.False(t, box.IsValid(), name)
	}
}

func TestGeoBoxRoundsOutwards(t *testing.T) {
	box := entities.GeoBox{MinLng: 13.1504, MinLat: -8.9501, MaxLng: 13.3496, MaxLat: -8.7499}.RoundedOut(0.001)

	assert.InDelta(t, 13.150, box.MinLng, 1e-9)
	assert.InDelta(t, -8.951, box.MinLat, 1e-9)
	assert.InDelta(t, 13.350, box.MaxLng, 1e-9)
	assert.InDelta(t, -8.749, box.MaxLat, 1e-9)
}

func TestGeoBoxArea(t *testing.T) {
	box := entities.GeoBox{MinLng: 13, MinLat: -9, MaxLng: 14, MaxLat: -8}
	assert.InDelta(t, 12_228, box.AreaKm2(), 5, "111.2 km by 110 km near Luanda")
}
