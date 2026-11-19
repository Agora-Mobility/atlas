package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
)

// The public page of a shared trip (Ariadne) places the pickup, the
// destination and the planned route on its map, and names the service.

type shareLive struct {
	repository.ILiveRepository
	share *repository.RequestShare
}

func (l shareLive) ShareByHash(context.Context, string) (*repository.RequestShare, error) {
	return l.share, nil
}

type serviceConfig struct {
	repository.IDispatchConfigRepository
	service *entities.DispatchService
}

func (c serviceConfig) FindService(context.Context, uuid.UUID) (*entities.DispatchService, error) {
	return c.service, nil
}

func TestPublicTripPlacesTheTripOnTheMap(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	serviceID := uuid.New()
	request := &entities.ServiceRequest{
		ID: uuid.New(), PublicCode: "TX-48271", Status: entities.RequestSearching, CategorySlug: "taxi", ServiceID: serviceID,
		Pickup:      entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.8147, Lng: 13.2302}, Address: "Rua da Missão, Luanda"},
		Destination: &entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.9167, Lng: 13.1833}, Address: "Talatona, Luanda"},
		Fare:        entities.RequestFare{RouteGeometry: "zmrv@cbdpA"},
	}
	u := NewUsecase(Deps{
		Live:     shareLive{share: &repository.RequestShare{ID: uuid.New(), RequestID: request.ID, ExpiresAt: now.Add(time.Hour)}},
		Requests: &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{request.ID: request}},
		Config:   serviceConfig{service: &entities.DispatchService{ID: serviceID, Name: "Táxi Económico"}},
	}, Settings{})
	u.now = func() time.Time { return now }

	trip, err := u.PublicTrip(context.Background(), "token")

	require.NoError(t, err)
	assert.Equal(t, "Táxi Económico", trip.ServiceName)
	require.NotNil(t, trip.Pickup)
	assert.InDelta(t, -8.8147, trip.Pickup.Lat, 1e-9)
	require.NotNil(t, trip.Destination)
	assert.InDelta(t, 13.1833, trip.Destination.Lng, 1e-9)
	assert.Equal(t, "zmrv@cbdpA", trip.RouteGeometry)
	assert.Equal(t, "Rua da Missão, Luanda", trip.PickupAddress)
	assert.Nil(t, trip.Position, "no provider yet, no position")
}

func TestPublicTripWithoutDestinationOrRoute(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	request := &entities.ServiceRequest{
		ID: uuid.New(), PublicCode: "RB-1", Status: entities.RequestScheduled, CategorySlug: "towing", ServiceID: uuid.New(),
		Pickup: entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.8147, Lng: 13.2302}},
	}
	u := NewUsecase(Deps{
		Live:     shareLive{share: &repository.RequestShare{ID: uuid.New(), RequestID: request.ID, ExpiresAt: now.Add(time.Hour)}},
		Requests: &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{request.ID: request}},
		Config:   serviceConfig{},
	}, Settings{})
	u.now = func() time.Time { return now }

	trip, err := u.PublicTrip(context.Background(), "token")

	require.NoError(t, err)
	assert.NotNil(t, trip.Pickup)
	assert.Nil(t, trip.Destination)
	assert.Empty(t, trip.RouteGeometry)
	assert.Empty(t, trip.ServiceName)
}

// A shared link is for following the trip while it happens: once the trip is
// over the page says so, as for a link that never existed.
func TestPublicTripEndsWithTheTrip(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for _, status := range []entities.RequestStatus{entities.RequestCompleted, entities.RequestCancelled, entities.RequestExpired} {
		request := &entities.ServiceRequest{ID: uuid.New(), Status: status, CategorySlug: "taxi", ServiceID: uuid.New()}
		u := NewUsecase(Deps{
			Live:     shareLive{share: &repository.RequestShare{ID: uuid.New(), RequestID: request.ID, ExpiresAt: now.Add(time.Hour)}},
			Requests: &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{request.ID: request}},
			Config:   serviceConfig{},
		}, Settings{})
		u.now = func() time.Time { return now }

		_, err := u.PublicTrip(context.Background(), "token")

		require.ErrorIs(t, err, entities.ErrShareNotFound, status)
	}
}

type sampleProviders struct {
	repository.IProviderRepository
	sample *entities.ProviderPosition
}

func (p sampleProviders) LastLocationSample(context.Context, uuid.UUID) (*entities.ProviderPosition, error) {
	return p.sample, nil
}

// The live position expires a few intervals after the last report; the newest
// sample stored for the trip stands in for it while it is recent.
func TestLivePositionFallsBackToTheLastSampleOfTheTrip(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	provider := uuid.New()
	request := &entities.ServiceRequest{ID: uuid.New(), Status: entities.RequestProviderEnRoute, ProviderID: &provider}
	at := func(sample *entities.ProviderPosition) *Usecase {
		u := NewUsecase(Deps{Locator: &fakeLocator{}, Providers: sampleProviders{sample: sample}}, Settings{})
		u.now = func() time.Time { return now }
		return u
	}
	recent := &entities.ProviderPosition{ProviderID: provider, Point: entities.GeoPoint{Lat: -8.82, Lng: 13.23}, RecordedAt: now.Add(-90 * time.Second)}

	position, err := at(recent).livePosition(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, recent, position)

	old := &entities.ProviderPosition{ProviderID: provider, Point: recent.Point, RecordedAt: now.Add(-11 * time.Minute)}
	position, err = at(old).livePosition(context.Background(), request)
	require.NoError(t, err)
	assert.Nil(t, position, "a sample older than 10 minutes says nothing about where the car is")

	other := &entities.ProviderPosition{ProviderID: uuid.New(), Point: recent.Point, RecordedAt: recent.RecordedAt}
	position, err = at(other).livePosition(context.Background(), request)
	require.NoError(t, err)
	assert.Nil(t, position, "the sample of a provider reassigned away is not the car of the trip")

	position, err = at(nil).livePosition(context.Background(), request)
	require.NoError(t, err)
	assert.Nil(t, position)
}
