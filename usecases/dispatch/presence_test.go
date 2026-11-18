package dispatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

// ADR 0023: the position event carries the cadence interval, the speed and
// the earlier points of the batch, so the customer app moves the car
// continuously.

func positionAt(at time.Time, lat, lng float64) entities.ProviderPosition {
	heading := 90.0
	return entities.ProviderPosition{Point: entities.GeoPoint{Lat: lat, Lng: lng}, Heading: &heading, RecordedAt: at}
}

func TestPositionPayloadSharesTheBatchIntervalAndSpeed(t *testing.T) {
	u := &Usecase{settings: Settings{RoadFactor: 1.3, AverageSpeedKmh: 30}}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	points := []entities.ProviderPosition{
		positionAt(now.Add(-6*time.Second), -8.8139, 13.2291),
		positionAt(now.Add(-3*time.Second), -8.8143, 13.2297),
		positionAt(now, -8.8147, 13.2302),
	}
	target := entities.GeoPoint{Lat: -8.82, Lng: 13.24}
	trip := entities.ActiveTrip{RequestID: uuid.New(), Target: &target}

	payload := u.buildPositionPayload(trip, points, nil, entities.DefaultCadencePolicy().For(entities.PhaseEnRoute, false, false))

	assert.Equal(t, -8.8147, payload.Lat)
	assert.Equal(t, 13.2302, payload.Lng)
	require.NotNil(t, payload.ETASecond)
	require.NotNil(t, payload.NextInS)
	assert.Equal(t, 7, *payload.NextInS)
	require.NotNil(t, payload.SpeedMps)
	// Last two points: about 70 m in 3 s
	assert.InDelta(t, 23, *payload.SpeedMps, 2)
	require.Len(t, payload.Points, 2)
	assert.Equal(t, int64(-6000), payload.Points[0].DtMs)
	assert.Equal(t, int64(-3000), payload.Points[1].DtMs)
	assert.Equal(t, -8.8139, payload.Points[0].Lat)
}

func TestPositionPayloadWithASinglePointUsesThePreviousPosition(t *testing.T) {
	u := &Usecase{settings: Settings{RoadFactor: 1.3, AverageSpeedKmh: 30}}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	previous := positionAt(now.Add(-10*time.Second), -8.8139, 13.2291)

	payload := u.buildPositionPayload(entities.ActiveTrip{RequestID: uuid.New()}, []entities.ProviderPosition{positionAt(now, -8.8147, 13.2302)}, &previous, entities.DefaultCadencePolicy().For(entities.PhaseInTrip, false, false))

	assert.Empty(t, payload.Points)
	assert.Nil(t, payload.ETASecond)
	require.NotNil(t, payload.NextInS)
	assert.Equal(t, 10, *payload.NextInS)
	require.NotNil(t, payload.SpeedMps)
	assert.Positive(t, *payload.SpeedMps)

	// Without an earlier position there is no speed
	alone := u.buildPositionPayload(entities.ActiveTrip{RequestID: uuid.New()}, []entities.ProviderPosition{positionAt(now, -8.8147, 13.2302)}, nil, entities.Cadence{})
	assert.Nil(t, alone.SpeedMps)
	assert.Nil(t, alone.NextInS)
}

func TestPositionPayloadKeepsTheLatestTwentyPoints(t *testing.T) {
	u := &Usecase{settings: Settings{RoadFactor: 1.3, AverageSpeedKmh: 30}}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	var points []entities.ProviderPosition
	for i := 30; i >= 0; i-- {
		points = append(points, positionAt(now.Add(-time.Duration(i)*time.Second), -8.81-float64(i)*0.0001, 13.23))
	}

	payload := u.buildPositionPayload(entities.ActiveTrip{RequestID: uuid.New()}, points, nil, entities.Cadence{})

	require.Len(t, payload.Points, sharedPointsMax)
	assert.Equal(t, int64(-20000), payload.Points[0].DtMs)
	assert.Equal(t, int64(-1000), payload.Points[len(payload.Points)-1].DtMs)
}

func TestUsecaseFallsBackToTheDefaultCadence(t *testing.T) {
	assert.Equal(t, entities.DefaultCadencePolicy(), (&Usecase{}).cadence())
	custom := entities.DefaultCadencePolicy()
	custom.ApproachInterval = 4 * time.Second
	assert.Equal(t, custom, (&Usecase{settings: Settings{Cadence: custom}}).cadence())
}
