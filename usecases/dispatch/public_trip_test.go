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
		ID: uuid.New(), PublicCode: "TX-48271", Status: entities.RequestCompleted, CategorySlug: "taxi", ServiceID: serviceID,
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
	assert.Nil(t, trip.Position, "a finished trip has no live position")
}

func TestPublicTripWithoutDestinationOrRoute(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	request := &entities.ServiceRequest{
		ID: uuid.New(), PublicCode: "RB-1", Status: entities.RequestCancelled, CategorySlug: "towing", ServiceID: uuid.New(),
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
