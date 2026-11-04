package entities_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

func catalogItem(kind entities.CatalogKind, code, attrs string) entities.CatalogItem {
	return entities.CatalogItem{ID: uuid.New(), Kind: kind, Code: code, Label: code, Attrs: json.RawMessage(attrs), IsActive: true}
}

func TestVolumeComesFromTheCatalog(t *testing.T) {
	sofa := catalogItem(entities.CatalogRemovalItem, "sofa", `{"volume_dm3": 1500}`)
	lines, total, err := entities.RemovalVolume([]entities.RemovalItemChoice{{Item: &sofa, Qty: 2}})
	require.NoError(t, err)
	assert.EqualValues(t, 3000, total)
	assert.EqualValues(t, 3000, lines[0].VolumeDm3)

	_, _, err = entities.RemovalVolume([]entities.RemovalItemChoice{{Item: nil, Qty: 1}})
	assert.ErrorIs(t, err, entities.ErrRemovalItemUnknown)
	vehicle := catalogItem(entities.CatalogRemovalVehicle, "van", `{}`)
	_, _, err = entities.RemovalVolume([]entities.RemovalItemChoice{{Item: &vehicle, Qty: 1}})
	assert.ErrorIs(t, err, entities.ErrRemovalItemUnknown, "only items count")
	_, _, err = entities.RemovalVolume([]entities.RemovalItemChoice{{Item: &sofa, Qty: 501}})
	assert.Error(t, err)
}

func TestRecommendedVehicleIsTheSmallestThatCarriesTheVolume(t *testing.T) {
	vehicles := []entities.CatalogItem{
		catalogItem(entities.CatalogRemovalVehicle, "big", `{"max_volume_dm3": 35000}`),
		catalogItem(entities.CatalogRemovalVehicle, "van", `{"max_volume_dm3": 6000}`),
		catalogItem(entities.CatalogRemovalVehicle, "mid", `{"max_volume_dm3": 18000}`),
	}
	assert.Equal(t, "van", entities.RecommendedVehicle(vehicles, 0).Code)
	assert.Equal(t, "van", entities.RecommendedVehicle(vehicles, 6000).Code)
	assert.Equal(t, "mid", entities.RecommendedVehicle(vehicles, 6001).Code)
	assert.Nil(t, entities.RecommendedVehicle(vehicles, 35001))
}

func TestQuoteAddsVehicleHelpersAndExtras(t *testing.T) {
	quote := entities.RemovalQuote{VehicleCents: 8_500_000, HelpersCents: 2 * 500_000, ExtrasCents: 1_500_000}
	assert.EqualValues(t, 11_000_000, quote.TotalCents())
}

func TestDurationGrowsWithTheVolume(t *testing.T) {
	vehicle := catalogItem(entities.CatalogRemovalVehicle, "van", `{"base_minutes": 120, "minutes_per_m3": 10}`)
	assert.EqualValues(t, 150, entities.RemovalDuration(&vehicle, 3000))
}

func TestWindowStartMustMatch(t *testing.T) {
	window := catalogItem(entities.CatalogRemovalWindow, "morning", `{"start_minute": 480, "end_minute": 600}`)
	assert.True(t, entities.WindowStart(&window, time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)))
	assert.False(t, entities.WindowStart(&window, time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC)))
	other := catalogItem(entities.CatalogRemovalItem, "sofa", `{"start_minute": 480}`)
	assert.False(t, entities.WindowStart(&other, time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)))
}

func TestTeamValidatesItsMembers(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	team, err := entities.NewTeam(id, []entities.TeamMember{{Name: " Paulo ", Role: entities.TeamRoleLead}}, now)
	require.NoError(t, err)
	assert.Equal(t, "Paulo", team[0].Name)
	_, err = entities.NewTeam(id, []entities.TeamMember{{Name: "", Role: entities.TeamRoleLead}}, now)
	assert.Error(t, err)
	_, err = entities.NewTeam(id, []entities.TeamMember{{Name: "X", Role: "boss"}}, now)
	assert.Error(t, err)
	_, err = entities.NewTeam(id, make([]entities.TeamMember, 21), now)
	assert.Error(t, err)
}
