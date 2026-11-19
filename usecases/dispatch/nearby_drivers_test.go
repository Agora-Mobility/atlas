package dispatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

func TestPublicDriverIDIsStableForTheDayAndHidesTheDriver(t *testing.T) {
	u := NewUsecase(Deps{}, Settings{NearbyIDKey: []byte("key")})
	driver := uuid.New()
	morning := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)

	id := u.publicDriverID(driver, morning)

	assert.Equal(t, id, u.publicDriverID(driver, morning.Add(10*time.Hour)), "the same car keeps its id during the day")
	assert.NotEqual(t, id, u.publicDriverID(driver, morning.Add(24*time.Hour)), "and gets another the next day")
	assert.NotEqual(t, id, u.publicDriverID(uuid.New(), morning))
	assert.NotContains(t, id, driver.String())
	assert.Len(t, id, 16)
	other := NewUsecase(Deps{}, Settings{NearbyIDKey: []byte("key")})
	assert.Equal(t, id, other.publicDriverID(driver, morning), "every instance with the same key agrees")
}

func TestNearestLiveKeepsTheRadiusNearestFirst(t *testing.T) {
	pickup := entities.GeoPoint{Lat: -8.8390, Lng: 13.2340}
	near, nearer, far := uuid.New(), uuid.New(), uuid.New()
	live := []repository.LiveCandidate{
		{ProviderID: near, Point: entities.GeoPoint{Lat: -8.8450, Lng: 13.2340}},
		{ProviderID: far, Point: entities.GeoPoint{Lat: -8.9500, Lng: 13.2340}},
		{ProviderID: nearer, Point: entities.GeoPoint{Lat: -8.8400, Lng: 13.2340}},
	}

	out := nearestLive(live, pickup, 3000, 10)

	require.Len(t, out, 2)
	assert.Equal(t, nearer, out[0].ProviderID)
	assert.Equal(t, near, out[1].ProviderID)
	assert.Len(t, nearestLive(live, pickup, 3000, 1), 1)
}

func TestServicesShowTheNearestDriverOfEach(t *testing.T) {
	economy, comfort := uuid.New(), uuid.New()
	view := &dtos.NearbyDriversView{}

	summarize(view, entities.AvailableDriver{ServiceID: economy, ServiceSlug: "economico"}, 300)
	summarize(view, entities.AvailableDriver{ServiceID: comfort, ServiceSlug: "conforto"}, 400)
	summarize(view, entities.AvailableDriver{ServiceID: economy, ServiceSlug: "economico"}, 120)

	require.Len(t, view.Services, 2)
	assert.Equal(t, dtos.NearbyService{ServiceID: economy, ServiceSlug: "economico", Drivers: 2, ETASecond: 120}, view.Services[0])
	assert.Equal(t, 1, view.Services[1].Drivers)
}

func TestPositionsOnTheMapAreRounded(t *testing.T) {
	assert.InDelta(t, -8.8392, roundCoordinate(-8.839234), 1e-12)
	assert.InDelta(t, 13.2343, roundCoordinate(13.23426), 1e-12)
	heading := 91.6
	assert.InDelta(t, 92, *roundHeading(&heading), 0)
	assert.Nil(t, roundHeading(nil))
}
