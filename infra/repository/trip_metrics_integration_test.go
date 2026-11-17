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
	"github.com/Agora-Mobility/atlas/infra/repository"
)

func TestTripMetricsReachTheBackofficeReview(t *testing.T) {
	app, owner := setupDBs(t)
	ctx := context.Background()
	pricing := repository.NewPricingRepository(app)
	requests := repository.NewRequestRepository(app)
	request, _ := seedRequest(t, app, owner)

	started := time.Now().UTC().Add(-30 * time.Minute)
	providers := repository.NewProviderRepository(app)
	providerID := uuid.New()
	require.NoError(t, providers.SaveLocationSample(ctx, providerID, request.ID, entities.GeoPoint{Lat: -8.84, Lng: 13.23}, started.Add(-time.Minute)))
	require.NoError(t, providers.SaveLocationSample(ctx, providerID, request.ID, entities.GeoPoint{Lat: -8.84, Lng: 13.23}, started.Add(time.Minute)))
	require.NoError(t, providers.SaveLocationSample(ctx, providerID, request.ID, entities.GeoPoint{Lat: -8.85, Lng: 13.23}, started.Add(2*time.Minute)))
	require.NoError(t, providers.SaveLocationSample(ctx, providerID, request.ID, entities.GeoPoint{Lat: -8.86, Lng: 13.23}, started.Add(3*time.Minute)))
	distance, samples, err := pricing.TrackedDistance(ctx, request.ID, started)
	require.NoError(t, err)
	assert.Equal(t, 3, samples)
	assert.InDelta(t, 2_212, distance, 15, "two steps of 0.01 degrees of latitude, without the sample before the start")

	metrics := entities.NewTripMetrics(entities.TripMetricsInput{
		RequestID: request.ID, EstimatedDistanceM: 8_400, EstimatedDurationS: 1_320, ActualDistanceM: 12_000, ActualDuration: 30 * time.Minute,
		Samples: samples, SampleEvery: 30 * time.Second, ReviewAboveBps: 3_000, RecordedAt: time.Now().UTC(),
	})
	require.NoError(t, pricing.SaveTripMetrics(ctx, metrics))
	stored, err := pricing.FindTripMetrics(ctx, request.ID)
	require.NoError(t, err)
	assert.True(t, stored.NeedsReview)
	assert.Equal(t, 3, stored.Samples)
	assert.Equal(t, metrics.DistanceDeviationBps, stored.DistanceDeviationBps)

	flagged, total, err := requests.AdminList(ctx, domain.AdminRequestFilter{RouteReview: true, Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, flagged, 1)
	assert.True(t, flagged[0].RouteReview)
	assert.Equal(t, metrics.DistanceDeviationBps, *flagged[0].DeviationBps)

	metrics.NeedsReview = false
	require.NoError(t, pricing.SaveTripMetrics(ctx, metrics))
	_, total, err = requests.AdminList(ctx, domain.AdminRequestFilter{RouteReview: true, Limit: 10})
	require.NoError(t, err)
	assert.Zero(t, total, "a reviewed deviation leaves the list")
	_, total, err = requests.AdminList(ctx, domain.AdminRequestFilter{Limit: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}
