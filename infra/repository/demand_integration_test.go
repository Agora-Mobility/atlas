//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	domain "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/repository"
)

func TestDemandPricingSnapshotsAndPendingRequests(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	demand := repository.NewDemandRepository(app)
	request, _ := seedRequest(t, app, owner)

	pricing := &entities.DemandPricing{
		ID: uuid.New(), ZoneID: request.ZoneID, CategoryID: request.CategoryID, Mode: entities.DemandOn, MaxFactorBps: 13_000,
		Tiers: []entities.DemandTier{{AboveRatioBps: 10_000, FactorBps: 11_000}}, UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, demand.SaveDemandPricing(ctx, pricing))
	changed := *pricing
	changed.ID, changed.Mode, changed.CommissionOnSurge = uuid.New(), entities.DemandObserve, true
	require.NoError(t, demand.SaveDemandPricing(ctx, &changed))

	stored, err := demand.FindDemandPricing(ctx, request.ZoneID, request.CategoryID)
	require.NoError(t, err)
	assert.Equal(t, pricing.ID, stored.ID, "one row per zone and category")
	assert.Equal(t, entities.DemandObserve, stored.Mode)
	assert.True(t, stored.CommissionOnSurge)
	assert.Equal(t, pricing.Tiers, stored.Tiers)
	all, err := demand.ListDemandPricing(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	pending, err := demand.PendingCustomers(ctx, request.ZoneID, request.CategoryID)
	require.NoError(t, err)
	assert.Equal(t, map[int64][]uuid.UUID{request.PickupH3R8: {request.CustomerID}}, pending)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, demand.SaveSnapshots(ctx, []entities.DemandSnapshot{
		{ZoneID: request.ZoneID, CategoryID: request.CategoryID, DemandArea: entities.DemandArea{Area: 42, Pending: 18, Free: 10, RatioBps: 17_272, FactorBps: 12_000, RecordedAt: now}},
		{ZoneID: request.ZoneID, CategoryID: request.CategoryID, DemandArea: entities.DemandArea{Area: 42, Pending: 2, Free: 1, RatioBps: 15_000, FactorBps: 11_000, RecordedAt: now.Add(-48 * time.Hour)}},
	}))
	recent, err := demand.ListSnapshots(ctx, request.ZoneID, request.CategoryID, now.Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, 12_000, recent[0].FactorBps)
	removed, err := demand.PurgeSnapshots(ctx, now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, removed)
}

func TestEstimateKeepsTheDemandFactorAndItsAdjustment(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	pricing := repository.NewPricingRepository(app)
	requests := repository.NewRequestRepository(app)
	request, _ := seedRequest(t, app, owner)

	tariff := &entities.ServiceTariff{ID: uuid.New(), ServiceID: request.ServiceID, Currency: "AOA", BaseFeeCents: 50_000, ValidFrom: time.Now()}
	require.NoError(t, pricing.ReplaceTariff(ctx, tariff))
	destination := entities.GeoPoint{Lat: -8.92, Lng: 13.18}
	estimate := &entities.FareEstimate{
		ID: uuid.New(), CustomerID: request.CustomerID, TariffID: tariff.ID, ServiceID: request.ServiceID, Origin: request.Pickup.Point, Destination: &destination,
		DistanceMeters: 8_400, DurationSeconds: 1_320, RouteSource: "google", Currency: "AOA", ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now(),
	}
	estimate.Apply(entities.QuoteFare(270_000, 2_000, entities.DemandQuote{FactorBps: 12_000}))
	require.NoError(t, pricing.CreateEstimate(ctx, estimate))
	found, err := pricing.FindEstimate(ctx, estimate.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.DemandQuote{FactorBps: 12_000}, found.Demand())
	assert.EqualValues(t, 54_000, found.DemandCents)

	plain := *estimate
	plain.ID, plain.DemandFactorBps = uuid.New(), 0
	require.NoError(t, pricing.CreateEstimate(ctx, &plain), "an estimate without demand stores the normal factor")

	require.NoError(t, requests.AddFareAdjustment(ctx, entities.FareAdjustment{RequestID: request.ID, Reason: entities.AdjustmentDemand, AmountCents: 54_000, CreatedAt: time.Now()}))
}

func TestDemandSignalsCountQuotesFreeProvidersAndFactors(t *testing.T) {
	rdb := redisClient(t)
	geo := h3index.New()
	signals := repository.NewDemandSignals(rdb)
	locator := repository.NewProviderLocator(rdb, geo)
	ctx := context.Background()
	key := domain.DemandKey{ZoneID: uuid.New(), Category: "taxi"}
	now := time.Now().UTC()

	claimed, err := signals.ClaimRefresh(ctx, time.Minute)
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = signals.ClaimRefresh(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, claimed, "only one instance refreshes each minute")

	alice, bruno, carla := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, signals.RecordQuote(ctx, key, 7, alice, now.Add(-2*time.Minute)))
	require.NoError(t, signals.RecordQuote(ctx, key, 7, alice, now))
	require.NoError(t, signals.RecordQuote(ctx, key, 7, bruno, now.Add(-time.Minute)))
	require.NoError(t, signals.RecordQuote(ctx, key, 9, carla, now.Add(-10*time.Minute)))
	quotes, err := signals.RecentQuotes(ctx, key, now.Add(-3*time.Minute), now)
	require.NoError(t, err)
	require.Len(t, quotes, 1)
	assert.ElementsMatch(t, []uuid.UUID{alice, bruno}, quotes[7], "a customer quoting again counts once")

	at := entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	free, reserved, busy := uuid.New(), uuid.New(), uuid.New()
	for _, provider := range []uuid.UUID{free, reserved, busy} {
		require.NoError(t, locator.SetOnline(ctx, provider, taxiIndex(), time.Minute))
		require.NoError(t, locator.UpdatePosition(ctx, entities.ProviderPosition{ProviderID: provider, Point: at, AccuracyM: 5, RecordedAt: now}, time.Minute))
	}
	_, err = locator.Reserve(ctx, reserved, uuid.New(), time.Minute)
	require.NoError(t, err)
	require.NoError(t, locator.SetTrip(ctx, busy, entities.ActiveTrip{RequestID: uuid.New()}))
	cell, err := geo.Cell(at, 7)
	require.NoError(t, err)
	count, err := signals.FreeProviders(ctx, "taxi", 7, []int64{cell})
	require.NoError(t, err)
	assert.Equal(t, 1, count, "reserved providers and providers on a trip are not free")

	factor, err := signals.Factor(ctx, key, 7)
	require.NoError(t, err)
	assert.Equal(t, entities.NeutralDemandFactorBps, factor)
	require.NoError(t, signals.Publish(ctx, key, map[int64]int{7: 17_000, 9: 10_400}, map[int64]int{7: 12_000}, time.Minute))
	factor, err = signals.Factor(ctx, key, 7)
	require.NoError(t, err)
	assert.Equal(t, 12_000, factor)
	ratios, err := signals.Ratios(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{7: 17_000, 9: 10_400}, ratios)

	require.NoError(t, signals.Publish(ctx, key, nil, nil, time.Minute))
	factors, err := signals.Factors(ctx, key)
	require.NoError(t, err)
	assert.Empty(t, factors, "switching a zone off clears its factors at once")
}
