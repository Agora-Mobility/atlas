package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
)

var pickupPoint = entities.GeoPoint{Lat: -8.84, Lng: 13.23}

type world struct {
	t        *testing.T
	usecase  *Usecase
	clock    time.Time
	policy   *entities.DispatchPolicy
	requests *fakeRequests
	offers   *fakeOffers
	provs    *fakeProviders
	locator  *fakeLocator
	outbox   *fakeOutbox
	channels *fakeChannels
	request  *entities.ServiceRequest
}

func taxi() *entities.DispatchPolicy {
	return &entities.DispatchPolicy{
		ID: uuid.New(), CandidateSource: entities.CandidateLive, H3Resolution: 7, RadiiKm: []float64{3, 5, 10}, OfferMode: entities.OfferModeSequential, BatchSize: 1,
		OfferTTLSeconds: 20, SearchTTLSeconds: 180, AckTimeoutSeconds: 3, AckGiveUpSeconds: 8, InitialChannels: []string{"sse"}, FallbackChannels: []string{"fcm"},
		Ranking: entities.RankingDistanceIdle, OnExhausted: entities.ExhaustedSuggestRetry, ReserveProvider: true, PricingMode: entities.PricingFixed, AllowBoost: true,
		TrackingStatuses: []entities.RequestStatus{entities.RequestProviderEnRoute, entities.RequestInProgress},
	}
}

func towing() *entities.DispatchPolicy {
	policy := taxi()
	policy.RadiiKm, policy.OfferMode, policy.BatchSize = []float64{10, 30, 80}, entities.OfferModeParallel, 3
	policy.OfferTTLSeconds, policy.SearchTTLSeconds, policy.AckTimeoutSeconds, policy.AckGiveUpSeconds = 45, 600, 5, 30
	policy.FallbackChannels, policy.Ranking, policy.OnExhausted = []string{"fcm", "sms"}, entities.RankingDistance, entities.ExhaustedAlertOperator
	return policy
}

func newWorld(t *testing.T, policy *entities.DispatchPolicy) *world {
	t.Helper()
	w := &world{
		t: t, clock: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), policy: policy,
		requests: &fakeRequests{byID: map[uuid.UUID]*entities.ServiceRequest{}}, offers: &fakeOffers{byID: map[uuid.UUID]*entities.RequestOffer{}},
		provs: &fakeProviders{eligible: map[uuid.UUID]entities.EligibleProvider{}}, locator: &fakeLocator{locks: map[uuid.UUID]uuid.UUID{}},
		outbox: &fakeOutbox{}, channels: &fakeChannels{},
	}
	w.usecase = NewUsecase(Deps{
		Config:   &fakeConfig{policy: policy, zone: &entities.ServiceZone{ID: uuid.New(), Currency: "AOA", Timezone: "Africa/Luanda"}},
		Vouchers: nil, Ratings: fakeRatings{}, Payments: fakePayments{}, Providers: w.provs, Requests: w.requests, Offers: w.offers, Locator: w.locator, Outbox: w.outbox, Tx: fakeTx{}, Audit: fakeAudit{}, Geo: fakeGeo{}, Channels: w.channels,
	}, Settings{ChannelTimeout: time.Second})
	w.usecase.now = func() time.Time { return w.clock }

	fare, commission := int64(171_500), int64(17_150)
	request, err := entities.NewServiceRequest(entities.NewRequestInput{
		ID: uuid.New(), CustomerID: uuid.New(), Service: entities.DispatchService{ID: uuid.New(), CategoryID: uuid.New(), CategorySlug: "taxi", RequiresDestination: false},
		Policy: policy, Zone: &entities.ServiceZone{ID: uuid.New()}, Pickup: entities.RequestPlace{Point: pickupPoint}, IdempotencyKey: "k",
		Fare: entities.RequestFare{Cents: &fare, CommissionCents: &commission, Currency: "AOA"}, Now: w.clock,
	})
	require.NoError(t, err)
	_, err = request.Transition(entities.RequestSearching, entities.SystemRequestActor(), "", w.clock)
	require.NoError(t, err)
	request.BeginSearch(policy, w.clock)
	w.requests.byID[request.ID] = request
	w.request = request
	return w
}

func (w *world) addLiveProvider(distanceDegrees float64, lastJob *time.Time) uuid.UUID {
	id := uuid.New()
	w.locator.live = append(w.locator.live, repository.LiveCandidate{ProviderID: id, Point: entities.GeoPoint{Lat: pickupPoint.Lat + distanceDegrees, Lng: pickupPoint.Lng}})
	w.provs.eligible[id] = entities.EligibleProvider{UserID: id, LastJobCompletedAt: lastJob, CreatedAt: w.clock.Add(-30 * 24 * time.Hour)}
	return id
}

func (w *world) advance() {
	w.t.Helper()
	require.NoError(w.t, w.usecase.Advance(context.Background(), w.request.ID))
}

func (w *world) openOffers() []*entities.RequestOffer {
	var open []*entities.RequestOffer
	for _, offer := range w.offers.byID {
		if offer.IsOpen() {
			open = append(open, offer)
		}
	}
	return open
}

func (w *world) onlyOffer() *entities.RequestOffer {
	w.t.Helper()
	open := w.openOffers()
	require.Len(w.t, open, 1)
	return open[0]
}

func (w *world) stepTo(at time.Time) {
	w.t.Helper()
	w.clock = at
	for _, offer := range w.openOffers() {
		require.NoError(w.t, w.usecase.runOfferStep(context.Background(), offer.ID))
	}
}

func TestSequentialOffersOnlyTheNearestAndReservesIt(t *testing.T) {
	w := newWorld(t, taxi())
	near := w.addLiveProvider(0.001, nil)
	w.addLiveProvider(0.01, nil)

	w.advance()

	offer := w.onlyOffer()
	assert.Equal(t, near, offer.ProviderID)
	assert.Equal(t, entities.RequestOffered, w.request.Status)
	assert.Equal(t, offer.ID, w.locator.locks[near])
	assert.EqualValues(t, 154_350, *offer.NetCents, "the offer shows the value after commission")
	assert.Equal(t, 1, w.outbox.count(events.OfferCreated))
}

func TestReservedProviderIsSkipped(t *testing.T) {
	w := newWorld(t, taxi())
	near := w.addLiveProvider(0.001, nil)
	far := w.addLiveProvider(0.01, nil)
	w.locator.locks[near] = uuid.New()

	w.advance()

	assert.Equal(t, far, w.onlyOffer().ProviderID)
}

func TestProviderWithAnActiveJobOrNotEligibleIsSkipped(t *testing.T) {
	w := newWorld(t, taxi())
	busy := w.addLiveProvider(0.001, nil)
	suspended := w.addLiveProvider(0.002, nil)
	free := w.addLiveProvider(0.01, nil)
	w.provs.active = []uuid.UUID{busy}
	delete(w.provs.eligible, suspended)

	w.advance()

	assert.Equal(t, free, w.onlyOffer().ProviderID)
}

func TestProviderOutsideTheRadiusIsNotOfferedEvenIfInTheCells(t *testing.T) {
	w := newWorld(t, taxi())
	w.policy.RadiiKm = []float64{3}
	w.addLiveProvider(0.05, nil)

	w.advance()

	assert.Empty(t, w.offers.byID)
	assert.Equal(t, entities.RequestSearching, w.request.Status)
	assert.Equal(t, w.clock.Add(5*time.Second), *w.request.NextActionAt)
}

func TestRadiusExpandsUntilACandidateAppears(t *testing.T) {
	w := newWorld(t, taxi())
	w.addLiveProvider(0.08, nil)

	w.advance()

	assert.Equal(t, 2, w.request.SearchRound, "only the 10 km ring reaches it")
	assert.Len(t, w.openOffers(), 1)
}

func TestWithinADistanceBandTheLongestIdleComesFirst(t *testing.T) {
	w := newWorld(t, taxi())
	recent := w.clock.Add(-time.Minute)
	longAgo := w.clock.Add(-5 * time.Hour)
	w.addLiveProvider(0.0005, &recent)
	idle := w.addLiveProvider(0.0009, &longAgo)

	w.advance()

	assert.Equal(t, idle, w.onlyOffer().ProviderID)
}

func TestDistanceBeatsIdleAcrossBands(t *testing.T) {
	w := newWorld(t, taxi())
	recent := w.clock.Add(-time.Minute)
	longAgo := w.clock.Add(-5 * time.Hour)
	near := w.addLiveProvider(0.0005, &recent)
	w.addLiveProvider(0.02, &longAgo)

	w.advance()

	assert.Equal(t, near, w.onlyOffer().ProviderID)
}

func TestParallelOffersTheBatchSize(t *testing.T) {
	w := newWorld(t, towing())
	for i := range 5 {
		w.addLiveProvider(0.001*float64(i+1), nil)
	}

	w.advance()

	assert.Len(t, w.openOffers(), 3)
	assert.Equal(t, 3, w.outbox.count(events.OfferCreated))
}

func TestNoAckFallsBackAtThreeSecondsAndGivesUpAtEight(t *testing.T) {
	w := newWorld(t, taxi())
	first := w.addLiveProvider(0.001, nil)
	second := w.addLiveProvider(0.01, nil)
	w.advance()
	start := w.clock
	offerID := w.onlyOffer().ID

	w.stepTo(start.Add(2 * time.Second))
	assert.Empty(t, w.channels.channels())

	w.stepTo(start.Add(3 * time.Second))
	require.Eventually(t, func() bool { return len(w.channels.channels()) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"fcm"}, w.channels.channels())

	w.stepTo(start.Add(8 * time.Second))
	assert.Equal(t, entities.OfferNoResponse, w.offers.byID[offerID].Status)
	assert.Contains(t, w.locator.released, first, "the silent provider is free again")
	assert.Equal(t, second, w.onlyOffer().ProviderID, "the next provider gets it without waiting")
}

func TestTowingEscalatesThroughFcmAndThenSms(t *testing.T) {
	w := newWorld(t, towing())
	w.addLiveProvider(0.001, nil)
	w.advance()
	start := w.clock

	w.stepTo(start.Add(5 * time.Second))
	w.stepTo(start.Add(10 * time.Second))

	require.Eventually(t, func() bool { return len(w.channels.channels()) == 2 }, time.Second, time.Millisecond)
	assert.ElementsMatch(t, []string{"fcm", "sms"}, w.channels.channels())
}

func TestAckStopsTheFallback(t *testing.T) {
	w := newWorld(t, taxi())
	provider := w.addLiveProvider(0.001, nil)
	w.advance()
	offer := w.onlyOffer()
	start := w.clock

	w.clock = start.Add(time.Second)
	require.NoError(t, w.usecase.MarkDelivered(context.Background(), provider, offer.ID, "sse"))
	require.NoError(t, w.usecase.MarkDelivered(context.Background(), provider, offer.ID, "fcm"))
	assert.Equal(t, 1, w.outbox.count(events.OfferDelivered), "a repeated ack records nothing new")

	w.stepTo(start.Add(10 * time.Second))
	assert.Empty(t, w.channels.channels())
	assert.Equal(t, entities.OfferDelivered, offer.Status)

	w.stepTo(start.Add(20 * time.Second))
	assert.Equal(t, entities.OfferExpired, offer.Status)
}

func TestAcceptAssignsAndCancelsTheSiblings(t *testing.T) {
	w := newWorld(t, towing())
	winner := w.addLiveProvider(0.001, nil)
	loser := w.addLiveProvider(0.002, nil)
	w.advance()
	var winnerOffer, loserOffer *entities.RequestOffer
	for _, offer := range w.openOffers() {
		if offer.ProviderID == winner {
			winnerOffer = offer
		} else {
			loserOffer = offer
		}
	}
	version := w.request.Version
	w.usecase.Routing = nil
	w.usecase.Devices = nil

	_, err := w.usecase.Accept(context.Background(), winner, winnerOffer.ID, version+5)
	require.ErrorIs(t, err, ErrRequestConflict, "a stale version loses")

	require.NoError(t, acceptIgnoringView(w, winner, winnerOffer.ID, version))
	assert.Equal(t, entities.RequestAccepted, w.request.Status)
	assert.Equal(t, winner, *w.request.ProviderID)
	assert.Equal(t, entities.OfferCancelled, loserOffer.Status)
	assert.Contains(t, w.locator.released, loser)
	assert.Contains(t, w.locator.released, winner)

	_, err = w.usecase.Accept(context.Background(), loser, loserOffer.ID, version)
	assert.Error(t, err, "accepting late never succeeds")
}

func acceptIgnoringView(w *world, providerID, offerID uuid.UUID, version int) error {
	err := w.usecase.withLockedOffer(context.Background(), providerID, offerID, func(ctx context.Context, r *entities.ServiceRequest, offer *entities.RequestOffer, p *entities.DispatchPolicy, now time.Time) error {
		if r.Version != version {
			return ErrRequestConflict
		}
		if err := offer.Respond(entities.OfferAccepted, now); err != nil {
			return err
		}
		change, err := r.Transition(entities.RequestAccepted, entities.RequestActor{Type: entities.RequestActorProvider, ID: &providerID}, "", now)
		if err != nil {
			return err
		}
		r.ProviderID = &providerID
		if err := w.usecase.recordTransition(ctx, r, change, p); err != nil {
			return err
		}
		released, err := w.usecase.closeOffers(ctx, r, &offer.ID, "another_provider_accepted", now)
		w.usecase.releaseAll(ctx, append(released, reservation{providerID: providerID, offerID: offer.ID}))
		return err
	})
	return err
}

func TestAcceptOfQuoteModeNeedsAQuote(t *testing.T) {
	policy := towing()
	policy.OfferMode, policy.PricingMode = entities.OfferModeQuote, entities.PricingQuote
	w := newWorld(t, policy)
	provider := w.addLiveProvider(0.001, nil)
	w.advance()

	_, err := w.usecase.Accept(context.Background(), provider, w.onlyOffer().ID, w.request.Version)
	assert.ErrorIs(t, err, ErrQuoteRequired)
}

func TestOfferOfAnotherProviderIsNotFound(t *testing.T) {
	w := newWorld(t, taxi())
	w.addLiveProvider(0.001, nil)
	w.advance()

	_, err := w.usecase.Accept(context.Background(), uuid.New(), w.onlyOffer().ID, w.request.Version)
	assert.ErrorIs(t, err, ErrOfferNotFound)
}

func TestSearchDeadlineFollowsThePolicy(t *testing.T) {
	cases := []struct {
		action   entities.ExhaustedAction
		status   entities.RequestStatus
		operator bool
		reason   string
	}{
		{entities.ExhaustedSuggestRetry, entities.RequestExpired, false, ""},
		{entities.ExhaustedAlertOperator, entities.RequestSearching, true, "no_provider"},
		{entities.ExhaustedOperatorQueue, entities.RequestSearching, true, "no_provider"},
	}
	for _, c := range cases {
		t.Run(string(c.action), func(t *testing.T) {
			policy := taxi()
			policy.OnExhausted = c.action
			w := newWorld(t, policy)
			w.clock = w.clock.Add(181 * time.Second)
			w.request.NextActionAt = &w.clock

			w.advance()

			assert.Equal(t, c.status, w.request.Status)
			assert.Equal(t, c.operator, w.request.NeedsOperator)
			assert.Equal(t, c.reason, w.request.OperatorReason)
			assert.Nil(t, w.request.NextActionAt)
		})
	}
}

func TestRemovalsExtendTheDeadlineOnceBeforeTheQueue(t *testing.T) {
	policy := taxi()
	policy.OnExhausted = entities.ExhaustedExtendOrQueue
	w := newWorld(t, policy)
	w.clock = w.clock.Add(181 * time.Second)
	w.request.NextActionAt = &w.clock

	w.advance()
	assert.True(t, w.request.SearchExtended)
	assert.False(t, w.request.NeedsOperator)
	assert.True(t, w.request.SearchDeadline.After(w.clock))

	w.clock = w.request.SearchDeadline.Add(time.Second)
	w.request.NextActionAt = &w.clock
	w.advance()
	assert.True(t, w.request.NeedsOperator)
	assert.Equal(t, "no_provider_after_extension", w.request.OperatorReason)
}

func TestRedisFailureLeavesTheRequestDueForTheWatcher(t *testing.T) {
	w := newWorld(t, taxi())
	w.addLiveProvider(0.001, nil)
	w.locator.failure = errBoom

	err := w.usecase.Advance(context.Background(), w.request.ID)

	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, entities.RequestSearching, w.request.Status)
	assert.Equal(t, w.clock, *w.request.NextActionAt, "the next tick tries again")
}

func TestWorkOnAlreadyHandledRequestIsANoOp(t *testing.T) {
	w := newWorld(t, taxi())
	future := w.clock.Add(time.Minute)
	w.request.NextActionAt = &future
	w.addLiveProvider(0.001, nil)

	w.advance()

	assert.Empty(t, w.offers.byID, "another instance already handled it")
}

func TestScheduledBecomesSearchingAndOffers(t *testing.T) {
	w := newWorld(t, taxi())
	w.request.Status = entities.RequestScheduled
	w.request.NextActionAt = &w.clock
	w.addLiveProvider(0.001, nil)

	w.advance()

	assert.Equal(t, entities.RequestOffered, w.request.Status)
	require.Len(t, w.requests.changes, 2)
	assert.Equal(t, entities.RequestSearching, w.requests.changes[0].To)
	assert.Equal(t, entities.RequestOffered, w.requests.changes[1].To)
}

func TestReconcileRecoversAStuckOfferedRequest(t *testing.T) {
	w := newWorld(t, taxi())
	w.addLiveProvider(0.001, nil)
	w.advance()
	offer := w.onlyOffer()
	offer.Close(entities.OfferExpired, w.clock)
	w.clock = *w.request.NextActionAt

	w.advance()

	assert.Equal(t, entities.RequestSearching, w.request.Status, "an instance died between closing the offer and moving on")
}

func TestChannelFailureDoesNotStopTheWatcher(t *testing.T) {
	w := newWorld(t, taxi())
	w.channels.err = errBoom
	w.addLiveProvider(0.001, nil)
	w.advance()
	start := w.clock

	w.stepTo(start.Add(3 * time.Second))

	require.Eventually(t, func() bool { return len(w.channels.channels()) == 1 }, time.Second, time.Millisecond)
	assert.Len(t, w.openOffers(), 1)
}

func TestRankByETAReordersTheBest(t *testing.T) {
	w := newWorld(t, taxi())
	found := []candidate{{distanceM: 100}, {distanceM: 200}}
	found[0].provider.UserID, found[1].provider.UserID = uuid.New(), uuid.New()
	w.usecase.Locator = etaLocator{fakeLocator: w.locator}
	w.usecase.Routing = etaRouter{etas: []time.Duration{time.Minute, time.Second}}

	ordered := w.usecase.rankByETA(context.Background(), w.request, found)

	assert.Equal(t, found[1].provider.UserID, ordered[0].provider.UserID)
}

func TestCoverageCandidatesRespectTheDeclaredRadius(t *testing.T) {
	policy := taxi()
	policy.CandidateSource, policy.ReserveProvider = entities.CandidateCoverage, false
	w := newWorld(t, policy)
	base, radius, small := entities.GeoPoint{Lat: -8.85, Lng: 13.23}, 30.0, 0.5
	inside, outside := uuid.New(), uuid.New()
	w.provs.covering = []uuid.UUID{inside, outside}
	w.provs.eligible[inside] = entities.EligibleProvider{UserID: inside, Base: &base, ServiceRadiusKm: &radius}
	w.provs.eligible[outside] = entities.EligibleProvider{UserID: outside, Base: &base, ServiceRadiusKm: &small}

	w.advance()

	assert.Equal(t, inside, w.onlyOffer().ProviderID)
	assert.Empty(t, w.locator.locks, "coverage providers are not reserved")
}

func TestZoneCacheUsesInteriorCellsWithoutPostGIS(t *testing.T) {
	zone := &entities.ServiceZone{ID: uuid.New(), Name: "Luanda"}
	cache := NewZoneCache(&fakeConfig{zone: zone}, fakeGeo{})

	found, err := cache.Resolve(context.Background(), pickupPoint)

	require.NoError(t, err)
	assert.Equal(t, zone.ID, found.ID)
}

func TestTrackingPhaseFollowsThePolicy(t *testing.T) {
	policy := taxi()
	policy.TrackingStatuses = []entities.RequestStatus{entities.RequestProviderEnRoute}
	request := &entities.ServiceRequest{Status: entities.RequestAccepted, Pickup: entities.RequestPlace{Point: pickupPoint}}
	assert.True(t, tripFor(request, policy).Tracked, "accepted counts as on the way")
	assert.Equal(t, entities.PhaseEnRoute, tripFor(request, policy).Phase())

	request.Status = entities.RequestInProgress
	assert.False(t, tripFor(request, policy).Tracked)
	assert.Equal(t, entities.PhaseOnSite, tripFor(request, policy).Phase())
}

func TestPhaseOfIdleProviderWithAnOffer(t *testing.T) {
	assert.Equal(t, entities.PhaseOffered, phaseOf(nil, true))
	assert.Equal(t, entities.PhaseIdle, phaseOf(nil, false))
}

func TestNearbyProvidersOfAnotherServiceDoNotHideTheRightOne(t *testing.T) {
	w := newWorld(t, taxi())
	for range maxCandidatesPerSearch + 10 {
		other := uuid.New()
		w.locator.live = append(w.locator.live, repository.LiveCandidate{ProviderID: other, Point: pickupPoint})
	}
	right := w.addLiveProvider(0.01, nil)

	w.advance()

	assert.Equal(t, right, w.onlyOffer().ProviderID)
}

func TestAProviderWhoLostTheServiceCannotAcceptAnOpenOffer(t *testing.T) {
	w := newWorld(t, taxi())
	provider := w.addLiveProvider(0.001, nil)
	w.advance()
	offer := w.onlyOffer()
	delete(w.provs.eligible, provider)

	_, err := w.usecase.Accept(context.Background(), provider, offer.ID, w.request.Version)

	assert.ErrorIs(t, err, ErrProviderNotApproved)
}
