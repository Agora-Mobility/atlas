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

var requestNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func taxiPolicy() *entities.DispatchPolicy {
	return &entities.DispatchPolicy{
		ID: uuid.New(), CandidateSource: entities.CandidateLive, H3Resolution: 7, RadiiKm: []float64{3, 5, 10},
		OfferMode: entities.OfferModeSequential, BatchSize: 1, OfferTTLSeconds: 20, SearchTTLSeconds: 180,
		AckTimeoutSeconds: 3, AckGiveUpSeconds: 8, InitialChannels: []string{"sse"}, FallbackChannels: []string{"fcm"},
		TrackingStatuses: []entities.RequestStatus{entities.RequestProviderEnRoute, entities.RequestInProgress},
		ReserveProvider:  true, MaxStops: 3, ScheduleSlotMin: 15, ScheduleMaxDays: 7, PricingMode: entities.PricingFixed, AllowBoost: true,
	}
}

func newInput() entities.NewRequestInput {
	return entities.NewRequestInput{
		ID: uuid.New(), CustomerID: uuid.New(),
		Service:        entities.DispatchService{ID: uuid.New(), CategoryID: uuid.New(), CategorySlug: "taxi", RequiresDestination: true},
		Policy:         taxiPolicy(),
		Zone:           &entities.ServiceZone{ID: uuid.New()},
		Pickup:         entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, Reference: " Junto à farmácia "},
		Destination:    &entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.92, Lng: 13.18}},
		IdempotencyKey: "key-1",
		Now:            requestNow,
	}
}

func newRequest(t *testing.T) *entities.ServiceRequest {
	t.Helper()
	request, err := entities.NewServiceRequest(newInput())
	require.NoError(t, err)
	return request
}

func TestNewRequestKeepsThePinAsTheTruth(t *testing.T) {
	request := newRequest(t)
	assert.Equal(t, entities.RequestCreated, request.Status)
	assert.Equal(t, 1, request.Version)
	assert.Equal(t, "Junto à farmácia", request.Pickup.Reference)
	assert.Equal(t, json.RawMessage("{}"), request.Details)
}

func TestNewRequestValidation(t *testing.T) {
	cases := map[string]func(*entities.NewRequestInput){
		"pin missing":              func(in *entities.NewRequestInput) { in.Pickup.Point = entities.GeoPoint{} },
		"destination required":     func(in *entities.NewRequestInput) { in.Destination = nil },
		"no idempotency key":       func(in *entities.NewRequestInput) { in.IdempotencyKey = " " },
		"passenger without phone":  func(in *entities.NewRequestInput) { in.PassengerName = "Maria" },
		"too many stops":           func(in *entities.NewRequestInput) { in.Stops = make([]entities.RequestPlace, 4) },
		"reference over the limit": func(in *entities.NewRequestInput) { in.Pickup.Reference = string(make([]rune, 141)) },
		"schedule in the past": func(in *entities.NewRequestInput) {
			past := requestNow.Add(-time.Hour)
			in.ScheduledFor = &past
		},
		"schedule past the horizon": func(in *entities.NewRequestInput) {
			far := requestNow.AddDate(0, 0, 8)
			in.ScheduledFor = &far
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := newInput()
			mutate(&in)
			_, err := entities.NewServiceRequest(in)
			assert.Error(t, err)
		})
	}
}

func TestMaintenanceIgnoresDestination(t *testing.T) {
	in := newInput()
	in.Service.RequiresDestination = false
	request, err := entities.NewServiceRequest(in)
	require.NoError(t, err)
	assert.Nil(t, request.Destination)
}

func TestScheduleNotAllowedByPolicy(t *testing.T) {
	in := newInput()
	in.Policy.ScheduleMaxDays = 0
	later := requestNow.Add(time.Hour)
	in.ScheduledFor = &later
	_, err := entities.NewServiceRequest(in)
	assert.ErrorIs(t, err, entities.ErrScheduleNotAllowed)
}

func TestHappyPathAdvancesVersionOnEveryStep(t *testing.T) {
	request := newRequest(t)
	provider := uuid.New()
	steps := []struct {
		to    entities.RequestStatus
		actor entities.RequestActorType
	}{
		{entities.RequestSearching, entities.RequestActorSystem},
		{entities.RequestOffered, entities.RequestActorSystem},
		{entities.RequestAccepted, entities.RequestActorProvider},
		{entities.RequestProviderEnRoute, entities.RequestActorProvider},
		{entities.RequestInProgress, entities.RequestActorProvider},
		{entities.RequestCompleted, entities.RequestActorProvider},
	}
	for i, step := range steps {
		change, err := request.Transition(step.to, entities.RequestActor{Type: step.actor, ID: &provider}, "", requestNow)
		require.NoError(t, err, step.to)
		assert.Equal(t, i+1, change.ExpectedVersion)
		assert.Equal(t, i+2, request.Version)
	}
	assert.NotNil(t, request.AcceptedAt)
	assert.NotNil(t, request.StartedAt)
	assert.NotNil(t, request.CompletedAt)
	assert.True(t, request.Status.IsTerminal())
}

func TestInvalidTransitionsAreRefused(t *testing.T) {
	request := newRequest(t)
	_, err := request.Transition(entities.RequestCompleted, entities.RequestActor{Type: entities.RequestActorProvider}, "", requestNow)
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)

	_, err = request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "", requestNow)
	require.NoError(t, err)
	_, err = request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "", requestNow)
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestActorsAreChecked(t *testing.T) {
	request := newRequest(t)
	_, err := request.Transition(entities.RequestSearching, entities.RequestActor{Type: entities.RequestActorCustomer}, "", requestNow)
	assert.ErrorIs(t, err, entities.ErrTransitionForbidden)
}

func TestCancelNeedsAReasonAndClearsDueActions(t *testing.T) {
	request := newRequest(t)
	next := requestNow
	request.NextActionAt = &next
	customer := entities.RequestActor{Type: entities.RequestActorCustomer}

	_, err := request.Transition(entities.RequestCancelled, customer, "  ", requestNow)
	assert.ErrorIs(t, err, entities.ErrCancelReasonMissing)

	_, err = request.Transition(entities.RequestCancelled, customer, "Mudei de planos", requestNow)
	require.NoError(t, err)
	assert.Equal(t, "customer", request.CancelledBy)
	assert.Nil(t, request.NextActionAt)
}

func TestInProgressCannotBeCancelled(t *testing.T) {
	request := newRequest(t)
	request.Status = entities.RequestInProgress
	_, err := request.Transition(entities.RequestCancelled, entities.RequestActor{Type: entities.RequestActorCustomer}, "x", requestNow)
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestBoostOnlyRaisesAndKeepsTheBase(t *testing.T) {
	request := newRequest(t)
	fare := int64(171_500)
	request.Fare.Cents = &fare

	added, err := request.ApplyBoost(entities.BoostTenPercentBps)
	require.NoError(t, err)
	assert.EqualValues(t, 17_150, added)
	assert.EqualValues(t, 188_650, *request.Fare.Cents)

	_, err = request.ApplyBoost(entities.BoostTenPercentBps)
	assert.ErrorIs(t, err, entities.ErrBoostNotHigher)

	added, err = request.ApplyBoost(entities.BoostTwentyPercentBps)
	require.NoError(t, err)
	assert.EqualValues(t, 17_150, added)
	assert.EqualValues(t, 205_800, *request.Fare.Cents)

	_, err = request.ApplyBoost(1500)
	assert.Error(t, err)
}

func TestBoostGoesEntirelyToTheProvider(t *testing.T) {
	fare, commission := int64(100_000), int64(10_000)
	request := newRequest(t)
	request.Fare.Cents, request.Fare.CommissionCents = &fare, &commission

	_, err := request.ApplyBoost(entities.BoostTwentyPercentBps)
	require.NoError(t, err)

	assert.EqualValues(t, 120_000, *request.Fare.Cents)
	assert.EqualValues(t, 10_000, *request.Fare.CommissionCents)
	assert.EqualValues(t, 110_000, *request.Fare.ProviderNetCents())
}

func TestOfferStepsFollowThePolicyClock(t *testing.T) {
	policy := taxiPolicy()
	offer := entities.NewRequestOffer(uuid.New(), uuid.New(), uuid.New(), policy, 850, nil, 0, requestNow)
	assert.Equal(t, requestNow.Add(3*time.Second), *offer.NextActionAt)

	step, _ := offer.DueStep(policy, requestNow.Add(2*time.Second))
	assert.Equal(t, entities.OfferStepNone, step)

	step, channel := offer.DueStep(policy, requestNow.Add(3*time.Second))
	assert.Equal(t, entities.OfferStepFallback, step)
	assert.Equal(t, "fcm", channel)

	offer.AdvanceFallback(policy)
	assert.Equal(t, requestNow.Add(8*time.Second), *offer.NextActionAt)
	step, _ = offer.DueStep(policy, requestNow.Add(8*time.Second))
	assert.Equal(t, entities.OfferStepGiveUp, step)
}

func TestDeliveredOfferOnlyWaitsForTheDeadline(t *testing.T) {
	policy := taxiPolicy()
	offer := entities.NewRequestOffer(uuid.New(), uuid.New(), uuid.New(), policy, 850, nil, 0, requestNow)

	first, err := offer.MarkDelivered("sse", policy, requestNow.Add(time.Second))
	require.NoError(t, err)
	assert.True(t, first)
	assert.Equal(t, offer.ExpiresAt, *offer.NextActionAt)

	second, err := offer.MarkDelivered("fcm", policy, requestNow.Add(2*time.Second))
	require.NoError(t, err)
	assert.False(t, second, "a repeated ack changes nothing")
	assert.Equal(t, "sse", offer.DeliveredVia)

	step, _ := offer.DueStep(policy, requestNow.Add(10*time.Second))
	assert.Equal(t, entities.OfferStepNone, step)
	step, _ = offer.DueStep(policy, offer.ExpiresAt)
	assert.Equal(t, entities.OfferStepExpire, step)
}

func TestOfferResponseRules(t *testing.T) {
	policy := taxiPolicy()
	offer := entities.NewRequestOffer(uuid.New(), uuid.New(), uuid.New(), policy, 850, nil, 0, requestNow)

	assert.ErrorIs(t, offer.Respond(entities.OfferAccepted, offer.ExpiresAt), entities.ErrOfferExpired)
	require.NoError(t, offer.Respond(entities.OfferRejected, requestNow.Add(time.Second)))
	assert.ErrorIs(t, offer.Respond(entities.OfferAccepted, requestNow.Add(2*time.Second)), entities.ErrOfferClosed)
	_, err := offer.MarkDelivered("sse", policy, requestNow.Add(2*time.Second))
	assert.ErrorIs(t, err, entities.ErrOfferClosed)
}

func TestQuoteIsValidatedAndCanBeRevised(t *testing.T) {
	policy := taxiPolicy()
	offer := entities.NewRequestOffer(uuid.New(), uuid.New(), uuid.New(), policy, 850, nil, 0, requestNow)
	validUntil := requestNow.Add(time.Hour)

	require.Error(t, offer.SubmitQuote(0, "", validUntil, requestNow))
	offer = entities.NewRequestOffer(uuid.New(), uuid.New(), uuid.New(), policy, 850, nil, 0, requestNow)
	require.NoError(t, offer.SubmitQuote(100, "ok", validUntil, requestNow))
	assert.Equal(t, entities.OfferQuoted, offer.Status)
	assert.Equal(t, validUntil, offer.ExpiresAt)
	require.NoError(t, offer.SubmitQuote(90, "melhor", validUntil, requestNow))
	assert.EqualValues(t, 90, *offer.QuoteCents)
}

func TestFareMatchesTheProposalExample(t *testing.T) {
	tariff := entities.ServiceTariff{BaseFeeCents: 30_000, PerKmFeeCents: 13_000, PerMinuteFeeCents: 1_500, CommissionBps: 1000}
	fare := tariff.FareFor(8000, 25*time.Minute, requestNow)
	assert.EqualValues(t, 171_500, fare)
	commission := tariff.CommissionFor(fare)
	assert.EqualValues(t, 17_150, commission)
	assert.EqualValues(t, 154_350, fare-commission)
}

func TestFareMinimumNightAndZoneFees(t *testing.T) {
	tariff := entities.ServiceTariff{
		BaseFeeCents: 100, MinimumFeeCents: 45_000, ZoneFeeCents: 500, NightFeeCents: 2_000, NightStartHour: 22, NightEndHour: 6,
	}
	day := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	assert.EqualValues(t, 45_500, tariff.FareFor(100, time.Minute, day), "the minimum applies before the zone fee")
	assert.EqualValues(t, 47_500, tariff.FareFor(100, time.Minute, day.Add(11*time.Hour)), "23h is night")
	assert.EqualValues(t, 47_500, tariff.FareFor(100, time.Minute, time.Date(2026, 11, 1, 5, 59, 0, 0, time.UTC)), "05h59 is still night")
	assert.EqualValues(t, 45_500, tariff.FareFor(100, time.Minute, time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC)))
}

func TestWaitingFeeOnlyAfterTheFreeWait(t *testing.T) {
	tariff := entities.ServiceTariff{PerMinuteFeeCents: 1_500, FreeWaitSeconds: 180}
	assert.Zero(t, tariff.WaitingFeeFor(3*time.Minute))
	assert.EqualValues(t, 3_000, tariff.WaitingFeeFor(5*time.Minute))
}

func TestEstimateMatchesTheSamePinOnly(t *testing.T) {
	destination := entities.GeoPoint{Lat: -8.92, Lng: 13.18}
	estimate := entities.FareEstimate{Origin: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, Destination: &destination, ExpiresAt: requestNow}

	assert.True(t, estimate.Matches(entities.GeoPoint{Lat: -8.8401, Lng: 13.2301}, &destination))
	assert.False(t, estimate.Matches(entities.GeoPoint{Lat: -8.85, Lng: 13.23}, &destination))
	assert.False(t, estimate.Matches(estimate.Origin, nil))
	assert.True(t, estimate.IsExpired(requestNow))
}

func TestCadenceTable(t *testing.T) {
	idle := entities.CadenceFor(entities.PhaseIdle, false, false)
	assert.Equal(t, 30*time.Second, idle.Interval)
	assert.Equal(t, 150, idle.Distance)
	assert.Equal(t, 75*time.Second, idle.PresenceTTL())

	assert.Equal(t, 7*time.Second, entities.CadenceFor(entities.PhaseEnRoute, false, false).Interval)
	assert.Equal(t, 10*time.Second, entities.CadenceFor(entities.PhaseInTrip, false, false).Interval)
	assert.Equal(t, 60*time.Second, entities.CadenceFor(entities.PhaseIdle, true, false).Interval)
	assert.Equal(t, 20*time.Second, entities.CadenceFor(entities.PhaseInTrip, false, true).Interval)
	assert.False(t, entities.CadenceFor(entities.PhaseOffline, false, false).SendsGPS)
	assert.False(t, entities.CadenceFor(entities.PhaseOnSite, false, false).SendsGPS)
}

func TestPlausibilityRejectsBadPointsAndImpossibleJumps(t *testing.T) {
	limits := entities.PlausibilityLimits{MaxAccuracyM: 100, MaxSpeedMps: 55}
	first := entities.ProviderPosition{Point: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, AccuracyM: 10, RecordedAt: requestNow}

	assert.True(t, limits.Accepts(first, nil))
	assert.False(t, limits.Accepts(entities.ProviderPosition{Point: first.Point, AccuracyM: 500, RecordedAt: requestNow}, nil))

	near := entities.ProviderPosition{Point: entities.GeoPoint{Lat: -8.841, Lng: 13.23}, AccuracyM: 10, RecordedAt: requestNow.Add(10 * time.Second)}
	assert.True(t, limits.Accepts(near, &first))

	jump := entities.ProviderPosition{Point: entities.GeoPoint{Lat: -9.84, Lng: 13.23}, AccuracyM: 10, RecordedAt: requestNow.Add(10 * time.Second)}
	assert.False(t, limits.Accepts(jump, &first))

	stale := near
	stale.RecordedAt = requestNow.Add(-time.Second)
	assert.False(t, limits.Accepts(stale, &first))
}

func TestPolicyValidation(t *testing.T) {
	cells := func(radius float64, _ int) int { return int(radius) * 10 }
	valid := taxiPolicy()
	valid.SearchTTLSeconds = 180
	require.NoError(t, valid.Validate(cells))

	wide := taxiPolicy()
	wide.RadiiKm = []float64{500}
	assert.Error(t, wide.Validate(cells))

	shrinking := taxiPolicy()
	shrinking.RadiiKm = []float64{5, 3}
	assert.Error(t, shrinking.Validate(cells))

	batch := taxiPolicy()
	batch.BatchSize = 3
	assert.Error(t, batch.Validate(cells))

	channel := taxiPolicy()
	channel.FallbackChannels = []string{"telegram"}
	assert.Error(t, channel.Validate(cells))
}

func TestEstimateRouteHasToMatchStopsAndSchedule(t *testing.T) {
	destination := entities.GeoPoint{Lat: -8.8, Lng: 13.2}
	stop := entities.GeoPoint{Lat: -8.82, Lng: 13.21}
	at := time.Date(2026, 11, 3, 7, 30, 0, 0, time.UTC)
	estimate := entities.FareEstimate{Origin: entities.GeoPoint{Lat: -8.84, Lng: 13.23}, Destination: &destination, Stops: []entities.GeoPoint{stop}, ScheduledFor: &at}

	assert.True(t, estimate.MatchesRoute(estimate.Origin, &destination, []entities.GeoPoint{stop}, &at))
	assert.False(t, estimate.MatchesRoute(estimate.Origin, &destination, nil, &at), "the estimate has one stop more")
	assert.False(t, estimate.MatchesRoute(estimate.Origin, &destination, []entities.GeoPoint{stop, stop}, &at), "the request has one stop more")
	assert.False(t, estimate.MatchesRoute(estimate.Origin, &destination, []entities.GeoPoint{{Lat: -8.9, Lng: 13.3}}, &at), "another stop")
	assert.False(t, estimate.MatchesRoute(estimate.Origin, &destination, []entities.GeoPoint{stop}, nil), "a price for later does not serve for now")
	later := at.Add(time.Hour)
	assert.False(t, estimate.MatchesRoute(estimate.Origin, &destination, []entities.GeoPoint{stop}, &later), "the price depends on the hour")
}

func TestStopFeeIsAddedAfterTheMinimum(t *testing.T) {
	tariff := entities.ServiceTariff{BaseFeeCents: 100, MinimumFeeCents: 500, StopFeeCents: 200}
	day := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	assert.EqualValues(t, 500, tariff.FareForRoute(0, 0, day, 0))
	assert.EqualValues(t, 900, tariff.FareForRoute(0, 0, day, 2))
}

func TestScheduleFollowsTheSlotsOfThePolicy(t *testing.T) {
	policy := entities.DispatchPolicy{ScheduleMaxDays: 7, ScheduleSlotMin: 15}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	assert.NoError(t, policy.ValidateSchedule(now.Add(30*time.Minute), now))
	assert.Error(t, policy.ValidateSchedule(now.Add(37*time.Minute), now), "07:37 is not a 15 minute slot")
	policy.ScheduleSlotMin = 1
	assert.NoError(t, policy.ValidateSchedule(now.Add(37*time.Minute), now), "the slot is configurable")
}

func TestRequestKeepsTheRouteOfItsQuoteAndReplacesItOnRouteChange(t *testing.T) {
	input := newInput()
	input.Fare = entities.RequestFare{RouteGeometry: "quoted"}
	request, err := entities.NewServiceRequest(input)
	require.NoError(t, err)
	assert.Equal(t, "quoted", request.Fare.RouteGeometry)

	destination := entities.RequestPlace{Point: entities.GeoPoint{Lat: -8.90, Lng: 13.17}}
	stops := []entities.RequestPlace{{Point: entities.GeoPoint{Lat: -8.86, Lng: 13.20}}}
	require.NoError(t, request.ChangeRoute(destination, stops, taxiPolicy(), 200_000, 20_000, "rerouted"))
	assert.Equal(t, "rerouted", request.Fare.RouteGeometry)
	assert.True(t, request.RouteChanged)
}
