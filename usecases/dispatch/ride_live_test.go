package dispatch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

type savedRideTokens struct {
	repository.IRidePushTokenRepository
	saved []entities.RidePushToken
}

func (s *savedRideTokens) Save(_ context.Context, token entities.RidePushToken) error {
	s.saved = append(s.saved, token)
	return nil
}

type rideLiveRecorder struct {
	mu        sync.Mutex
	positions []entities.RideLiveState
	done      chan struct{}
}

func (r *rideLiveRecorder) Position(_ context.Context, state entities.RideLiveState) error {
	r.mu.Lock()
	r.positions = append(r.positions, state)
	r.mu.Unlock()
	r.done <- struct{}{}
	return nil
}

func (r *rideLiveRecorder) Refresh(context.Context, uuid.UUID) error { return nil }

type claimOnce struct {
	repository.IProviderLocator
	claimed map[uuid.UUID]bool
}

func (c *claimOnce) ClaimRideLivePush(_ context.Context, requestID uuid.UUID, _ time.Duration) (bool, error) {
	if c.claimed[requestID] {
		return false, nil
	}
	c.claimed[requestID] = true
	return true, nil
}

func rideRequest(status entities.RequestStatus) *entities.ServiceRequest {
	providerID := uuid.New()
	return &entities.ServiceRequest{ID: uuid.New(), CustomerID: uuid.New(), ProviderID: &providerID, Status: status}
}

func TestCustomerRegistersTheLockScreenTokenOfTheRide(t *testing.T) {
	request := rideRequest(entities.RequestProviderEnRoute)
	tokens := &savedRideTokens{}
	u := NewUsecase(Deps{Requests: &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{request.ID: request}}, RideTokens: tokens}, Settings{})

	err := u.RegisterRidePushToken(context.Background(), request.CustomerID, request.ID, dtos.RidePushTokenInput{Platform: "ios", Token: "activity-token"})

	require.NoError(t, err)
	require.Len(t, tokens.saved, 1)
	assert.Equal(t, entities.RidePushToken{RequestID: request.ID, UserID: request.CustomerID, Platform: "ios", Token: "activity-token"}, tokens.saved[0])
}

func TestOnlyTheCustomerOfAnActiveRideRegistersAToken(t *testing.T) {
	active := rideRequest(entities.RequestInProgress)
	ended := rideRequest(entities.RequestCompleted)
	tokens := &savedRideTokens{}
	u := NewUsecase(Deps{
		Requests:   &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{active.ID: active, ended.ID: ended}},
		RideTokens: tokens,
	}, Settings{})
	in := dtos.RidePushTokenInput{Platform: "android", Token: "fcm"}

	assert.ErrorIs(t, u.RegisterRidePushToken(context.Background(), *active.ProviderID, active.ID, in), ErrRequestNotFound, "the driver is not the customer")
	assert.ErrorIs(t, u.RegisterRidePushToken(context.Background(), uuid.New(), active.ID, in), ErrRequestNotFound)
	assert.ErrorIs(t, u.RegisterRidePushToken(context.Background(), ended.CustomerID, ended.ID, in), entities.ErrRideLiveClosed)
	assert.Empty(t, tokens.saved)
}

func TestTrackedPositionGoesToTheLockScreenAtMostOncePerInterval(t *testing.T) {
	recorder := &rideLiveRecorder{done: make(chan struct{}, 4)}
	u := NewUsecase(Deps{Locator: &claimOnce{claimed: map[uuid.UUID]bool{}}, RideLive: recorder},
		Settings{RideLiveInterval: 5 * time.Second, ChannelTimeout: time.Second, RoadFactor: 1.4, AverageSpeedKmh: 25})
	target := entities.GeoPoint{Lat: -8.8383, Lng: 13.2344}
	trip := entities.ActiveTrip{RequestID: uuid.New(), CustomerID: uuid.New(), Status: entities.RequestProviderEnRoute, Target: &target, Tracked: true, Arrived: true}
	eta := 120

	u.pushRideLivePosition(context.Background(), trip, positionAt(time.Now(), -8.8450, 13.2344), &eta)
	u.pushRideLivePosition(context.Background(), trip, positionAt(time.Now(), -8.8449, 13.2344), &eta)

	select {
	case <-recorder.done:
	case <-time.After(time.Second):
		t.Fatal("the position was not sent")
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	require.Len(t, recorder.positions, 1, "the second point is inside the interval")
	state := recorder.positions[0]
	assert.Equal(t, trip.RequestID, state.RequestID)
	assert.Equal(t, entities.RidePhaseArrived, state.Phase)
	assert.Equal(t, &eta, state.ETASecond)
	require.NotNil(t, state.DistanceM)
	assert.InDelta(t, 1040, *state.DistanceM, 20)
	require.NotNil(t, state.Driver)
	assert.InDelta(t, -8.8450, state.Driver.Lat, 1e-9)
}

func TestPositionUpdatesAreOffWithoutAnInterval(t *testing.T) {
	recorder := &rideLiveRecorder{done: make(chan struct{}, 1)}
	// A locator without ClaimRideLivePush would panic if it were called
	u := NewUsecase(Deps{RideLive: recorder}, Settings{})

	u.pushRideLivePosition(context.Background(), entities.ActiveTrip{RequestID: uuid.New()}, positionAt(time.Now(), -8.8, 13.2), nil)

	assert.Empty(t, recorder.positions)
}
