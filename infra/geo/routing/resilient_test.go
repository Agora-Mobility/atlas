package routing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/geo/routing"
)

var (
	from = entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	to   = entities.GeoPoint{Lat: -8.90, Lng: 13.23}
)

type stubRouter struct {
	err   error
	calls int
	wait  time.Duration
}

func (s *stubRouter) Route(ctx context.Context, _, _ entities.GeoPoint, _ ...entities.GeoPoint) (services.Route, error) {
	s.calls++
	if s.wait > 0 {
		select {
		case <-time.After(s.wait):
		case <-ctx.Done():
			return services.Route{}, ctx.Err()
		}
	}
	if s.err != nil {
		return services.Route{}, s.err
	}
	return services.Route{DistanceMeters: 1, Duration: time.Second, Source: services.RouteSourceMapbox}, nil
}

func (s *stubRouter) ETAs(context.Context, []entities.GeoPoint, entities.GeoPoint) ([]time.Duration, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return []time.Duration{time.Second}, nil
}

func settings() routing.Settings {
	return routing.Settings{Timeout: 50 * time.Millisecond, FailureThreshold: 2, Cooldown: time.Hour, RoadFactor: 1.4, AverageSpeedKmh: 25}
}

func TestPrimaryRouteIsUsedWhileHealthy(t *testing.T) {
	route, err := routing.NewResilient(&stubRouter{}, settings()).Route(context.Background(), from, to)
	require.NoError(t, err)
	assert.Equal(t, services.RouteSourceMapbox, route.Source)
}

func TestFailureFallsBackToRoadFactor(t *testing.T) {
	route, err := routing.NewResilient(&stubRouter{err: errors.New("boom")}, settings()).Route(context.Background(), from, to)

	require.NoError(t, err)
	assert.Equal(t, services.RouteSourceFallback, route.Source)
	straight := entities.DistanceMeters(from, to)
	assert.InDelta(t, straight*1.4, route.DistanceMeters, 1)
	assert.InDelta(t, straight*1.4/(25*1000.0/3600), route.Duration.Seconds(), 1)
}

func TestWithoutPrimaryEveryRouteIsAFallback(t *testing.T) {
	route, err := routing.NewResilient(nil, settings()).Route(context.Background(), from, to, entities.GeoPoint{Lat: -8.87, Lng: 13.3})
	require.NoError(t, err)
	assert.Equal(t, services.RouteSourceFallback, route.Source)
}

func TestSlowPrimaryIsCutByTheTimeout(t *testing.T) {
	start := time.Now()
	route, err := routing.NewResilient(&stubRouter{wait: time.Minute}, settings()).Route(context.Background(), from, to)
	require.NoError(t, err)
	assert.Equal(t, services.RouteSourceFallback, route.Source)
	assert.Less(t, time.Since(start), time.Second)
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	primary := &stubRouter{err: errors.New("boom")}
	resilient := routing.NewResilient(primary, settings())

	for range 5 {
		_, err := resilient.Route(context.Background(), from, to)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, primary.calls, "after the threshold the provider is left alone")
}

func TestETAsFallBackForEveryOrigin(t *testing.T) {
	etas, err := routing.NewResilient(&stubRouter{err: errors.New("boom")}, settings()).ETAs(context.Background(), []entities.GeoPoint{from, to}, to)
	require.NoError(t, err)
	assert.Len(t, etas, 2)
	assert.Zero(t, etas[1])
}
