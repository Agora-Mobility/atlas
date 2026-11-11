package entities

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestTripMetricsFlagLongDetoursForReview(t *testing.T) {
	now := time.Now()
	in := TripMetricsInput{
		RequestID: uuid.New(), EstimatedDistanceM: 8_400, EstimatedDurationS: 1_320, ActualDistanceM: 11_760, ActualDuration: 30 * time.Minute,
		Samples: 60, SampleEvery: 30 * time.Second, ReviewAboveBps: 3_000, RecordedAt: now,
	}

	metrics := NewTripMetrics(in)
	assert.Equal(t, 4_000, metrics.DistanceDeviationBps)
	assert.Equal(t, 3_636, metrics.DurationDeviationBps)
	assert.Equal(t, 1_800, metrics.ActualDurationS)
	assert.Equal(t, 60, metrics.Samples)
	assert.True(t, metrics.NeedsReview)

	in.RouteChanged = true
	assert.False(t, NewTripMetrics(in).NeedsReview, "the customer asked for the new route")

	in.RouteChanged, in.ActualDistanceM = false, 9_000
	assert.False(t, NewTripMetrics(in).NeedsReview, "within the limit")

	in.EstimatedDistanceM = 0
	assert.Equal(t, 0, NewTripMetrics(in).DistanceDeviationBps)
}

func TestTripMetricsTrustAShortTripOnlyWithEnoughGPS(t *testing.T) {
	in := TripMetricsInput{
		EstimatedDistanceM: 8_400, ActualDistanceM: 4_200, ActualDuration: 20 * time.Minute,
		Samples: 40, SampleEvery: 30 * time.Second, ReviewAboveBps: 3_000,
	}
	shorter := NewTripMetrics(in)
	assert.Equal(t, -5_000, shorter.DistanceDeviationBps)
	assert.True(t, shorter.NeedsReview, "full coverage: the ride really was short")

	in.Samples = 23
	assert.False(t, NewTripMetrics(in).NeedsReview, "gaps in the GPS make a ride look shorter than it was")

	in.Samples = 24
	assert.True(t, NewTripMetrics(in).NeedsReview, "60% coverage is enough")

	in.Samples, in.ActualDistanceM = 2, 12_000
	assert.True(t, NewTripMetrics(in).NeedsReview, "gaps only shorten the distance, so a long one is real")
}
