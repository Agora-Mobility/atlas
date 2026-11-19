package notifications_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type fakeRideTokens struct {
	tokens  map[uuid.UUID][]entities.RidePushToken
	removed []string
	cleared []uuid.UUID
}

func (f *fakeRideTokens) Save(_ context.Context, token entities.RidePushToken) error {
	f.tokens[token.RequestID] = append(f.tokens[token.RequestID], token)
	return nil
}
func (f *fakeRideTokens) OfRequest(_ context.Context, requestID uuid.UUID) ([]entities.RidePushToken, error) {
	return f.tokens[requestID], nil
}
func (f *fakeRideTokens) DeleteOfRequest(_ context.Context, requestID uuid.UUID) error {
	f.cleared = append(f.cleared, requestID)
	delete(f.tokens, requestID)
	return nil
}
func (f *fakeRideTokens) Remove(_ context.Context, token string) error {
	f.removed = append(f.removed, token)
	return nil
}

type fakeRideRequests struct {
	repository.IRequestRepository
	request *entities.ServiceRequest
}

func (f fakeRideRequests) Find(context.Context, uuid.UUID) (*entities.ServiceRequest, error) {
	return f.request, nil
}

type fakeRideLocator struct {
	repository.IProviderLocator
	position *entities.ProviderPosition
}

func (f fakeRideLocator) LastPosition(context.Context, uuid.UUID) (*entities.ProviderPosition, error) {
	return f.position, nil
}

type fakeLiveActivity struct {
	sent []services.LiveActivityMessage
	err  error
}

func (f *fakeLiveActivity) Send(_ context.Context, message services.LiveActivityMessage) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.sent = append(f.sent, message)
	return "apns-1", nil
}

type rideFixture struct {
	notifier *notifications.RideLiveNotifier
	tokens   *fakeRideTokens
	push     *fakePush
	live     *fakeLiveActivity
	devices  *fakeDevices
	request  *entities.ServiceRequest
}

var (
	pickup      = entities.GeoPoint{Lat: -8.8383, Lng: 13.2344}
	destination = entities.GeoPoint{Lat: -8.8147, Lng: 13.2302}
	driverAt    = entities.GeoPoint{Lat: -8.8450, Lng: 13.2344}
)

func newRideFixture(status entities.RequestStatus) *rideFixture {
	providerID := uuid.New()
	request := &entities.ServiceRequest{
		ID: uuid.New(), CustomerID: uuid.New(), ProviderID: &providerID, Status: status,
		Pickup: entities.RequestPlace{Point: pickup}, Destination: &entities.RequestPlace{Point: destination},
	}
	tokens := &fakeRideTokens{tokens: map[uuid.UUID][]entities.RidePushToken{request.ID: {
		{RequestID: request.ID, UserID: request.CustomerID, Platform: entities.PlatformAndroid, Token: "fcm-token"},
		{RequestID: request.ID, UserID: request.CustomerID, Platform: entities.PlatformIOS, Token: "activity-token"},
	}}}
	f := &rideFixture{tokens: tokens, push: &fakePush{}, live: &fakeLiveActivity{}, devices: &fakeDevices{}, request: request}
	heading := 90.0
	f.notifier = notifications.NewRideLiveNotifier(tokens, fakeRideRequests{request: request},
		fakeRideLocator{position: &entities.ProviderPosition{Point: driverAt, Heading: &heading, RecordedAt: time.Now()}},
		f.devices, f.push, f.live, notifications.RideLiveSettings{RoadFactor: 1.4, AverageSpeedKmh: 25, StaleAfter: 2 * time.Minute, DismissAfter: 5 * time.Minute})
	return f
}

func transition(t *testing.T, request *entities.ServiceRequest, to entities.RequestStatus, at time.Time) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(events.RequestTransitionV1{
		Type: events.TransitionEventType(string(to)), RequestID: request.ID, CustomerID: request.CustomerID, ProviderID: request.ProviderID,
		To: string(to), OccurredAt: at,
	}, "")
	require.NoError(t, err)
	return env
}

func TestStatusChangeUpdatesTheLockScreenOnBothPlatforms(t *testing.T) {
	f := newRideFixture(entities.RequestProviderEnRoute)
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestProviderEnRoute, at)))

	require.Len(t, f.push.sent, 1)
	android := f.push.sent[0]
	assert.Equal(t, "fcm-token", android.Token)
	assert.Empty(t, android.Title, "data only, the app draws the notification")
	assert.Empty(t, android.Body)
	assert.True(t, android.HighPriority)
	assert.Equal(t, "ride.live", android.Data["type"])
	assert.Equal(t, "update", android.Data["event"])
	assert.Equal(t, "provider_en_route", android.Data["status"])
	assert.Equal(t, "en_route", android.Data["phase"])
	assert.Equal(t, "-8.845000", android.Data["lat"])
	assert.Equal(t, "90.0", android.Data["heading"])
	assert.NotEmpty(t, android.Data["eta_s"])
	assert.NotEmpty(t, android.Data["distance_m"])

	require.Len(t, f.live.sent, 1)
	ios := f.live.sent[0]
	assert.Equal(t, "activity-token", ios.Token)
	assert.Equal(t, services.LiveActivityUpdate, ios.Event)
	assert.True(t, ios.HighPriority)
	assert.Equal(t, at, ios.Timestamp)
	require.NotNil(t, ios.StaleAt)
	assert.Equal(t, at.Add(2*time.Minute), *ios.StaleAt)
	assert.Equal(t, "Motorista a caminho", ios.AlertTitle)
	assert.Empty(t, f.tokens.cleared)
	assert.Len(t, f.devices.records, 2, "status changes are recorded")
}

func TestStateOfLiveActivityHasTheKeysOfTheSwiftContentState(t *testing.T) {
	f := newRideFixture(entities.RequestInProgress)

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestInProgress, time.Now())))

	require.Len(t, f.live.sent, 1)
	state := fmt.Sprintf("%+v", f.live.sent[0].ContentState)
	for _, key := range []string{"Status:in_progress", "Phase:in_trip", "ETASeconds:", "DistanceMeters:", "DriverLat:", "UpdatedAt:"} {
		assert.Contains(t, state, key)
	}
}

func TestDistanceDuringTheTripIsToTheDestination(t *testing.T) {
	f := newRideFixture(entities.RequestInProgress)

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestInProgress, time.Now())))

	toDestination, _ := entities.StraightETA(driverAt, destination, 1.4, 25)
	assert.Equal(t, fmt.Sprint(toDestination), f.push.sent[0].Data["distance_m"])
}

func TestArrivalShowsArrived(t *testing.T) {
	f := newRideFixture(entities.RequestProviderEnRoute)
	arrived := time.Now()
	f.request.ArrivedAt = &arrived

	require.NoError(t, f.notifier.Refresh(context.Background(), f.request.ID))

	assert.Equal(t, "arrived", f.push.sent[0].Data["phase"])
	assert.Equal(t, "O motorista chegou", f.live.sent[0].AlertTitle)
}

func TestCompletedRideEndsTheLiveActivityAndForgetsTheTokens(t *testing.T) {
	f := newRideFixture(entities.RequestCompleted)
	at := time.Date(2026, 11, 19, 10, 30, 0, 0, time.UTC)

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestCompleted, at)))

	require.Len(t, f.live.sent, 1)
	ios := f.live.sent[0]
	assert.Equal(t, services.LiveActivityEnd, ios.Event)
	require.NotNil(t, ios.DismissAt)
	assert.Equal(t, at.Add(5*time.Minute), *ios.DismissAt, "a finished ride stays a little on the lock screen")
	assert.Nil(t, ios.StaleAt)
	assert.Equal(t, "end", f.push.sent[0].Data["event"])
	assert.Equal(t, "completed", f.push.sent[0].Data["status"])
	assert.Equal(t, []uuid.UUID{f.request.ID}, f.tokens.cleared)
}

func TestCancelledRideIsDismissedAtOnce(t *testing.T) {
	f := newRideFixture(entities.RequestCancelled)
	at := time.Now().UTC()

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestCancelled, at)))

	require.Len(t, f.live.sent, 1)
	assert.Equal(t, at, *f.live.sent[0].DismissAt)
	assert.Equal(t, "cancelled", f.push.sent[0].Data["phase"])
	assert.Equal(t, []uuid.UUID{f.request.ID}, f.tokens.cleared)
}

func TestEndThatFailsKeepsTheTokensForTheRetry(t *testing.T) {
	f := newRideFixture(entities.RequestCompleted)
	f.live.err = errors.New("apns answered 503")

	err := f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestCompleted, time.Now()))

	require.Error(t, err)
	assert.Empty(t, f.tokens.cleared)
}

func TestTokenTheProviderNoLongerKnowsIsRemoved(t *testing.T) {
	f := newRideFixture(entities.RequestProviderEnRoute)
	f.live.err = fmt.Errorf("%w: Unregistered", services.ErrPushTokenInvalid)

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestProviderEnRoute, time.Now())))

	assert.Equal(t, []string{"activity-token"}, f.tokens.removed)
}

func TestPositionUpdateIsLowPriorityOnIOSAndNotRecorded(t *testing.T) {
	f := newRideFixture(entities.RequestInProgress)
	eta, distance := 300, 2000
	state := entities.RideLiveState{
		RequestID: f.request.ID, Status: entities.RequestInProgress, Phase: entities.RidePhaseInTrip,
		ETASecond: &eta, DistanceM: &distance, Driver: &driverAt, At: time.Now(),
	}

	require.NoError(t, f.notifier.Position(context.Background(), state))

	require.Len(t, f.live.sent, 1)
	assert.False(t, f.live.sent[0].HighPriority)
	assert.Empty(t, f.live.sent[0].AlertTitle, "a position update does not light the screen")
	assert.Equal(t, 60, f.live.sent[0].TTLSeconds)
	assert.Equal(t, "300", f.push.sent[0].Data["eta_s"])
	assert.Equal(t, "2000", f.push.sent[0].Data["distance_m"])
	assert.Empty(t, f.devices.records)
}

func TestRideWithoutTokensSendsNothing(t *testing.T) {
	f := newRideFixture(entities.RequestInProgress)
	f.tokens.tokens = map[uuid.UUID][]entities.RidePushToken{}

	require.NoError(t, f.notifier.OnEvent(context.Background(), transition(t, f.request, entities.RequestCompleted, time.Now())))
	require.NoError(t, f.notifier.Refresh(context.Background(), f.request.ID))

	assert.Empty(t, f.push.sent)
	assert.Empty(t, f.live.sent)
}

func TestRideLiveListensToTheTransitionsTheLockScreenShows(t *testing.T) {
	keys := notifications.RideLiveBindingKeys()
	assert.Contains(t, keys, "requests.accepted.v1")
	assert.Contains(t, keys, "requests.completed.v1")
	assert.Contains(t, keys, "requests.cancelled.v1")
	assert.NotContains(t, keys, "requests.created.v1")
}
