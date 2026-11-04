package mapbox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/geo/mapbox"
)

var (
	pickup      = entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	destination = entities.GeoPoint{Lat: -8.92, Lng: 13.18}
)

func serve(t *testing.T, handler http.HandlerFunc) *mapbox.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return mapbox.NewClient("pk.test", server.URL, time.Second)
}

func TestRouteUsesTrafficProfileAndReadsDistanceAndDuration(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/directions/v5/mapbox/driving-traffic/13.230000,-8.840000;13.100000,-8.900000;13.180000,-8.920000")
		assert.Equal(t, "pk.test", r.URL.Query().Get("access_token"))
		assert.Equal(t, "simplified", r.URL.Query().Get("overview"))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "routes": []map[string]any{{"distance": 8000.4, "duration": 1500.0, "geometry": "abc"}}})
	})

	route, err := client.Route(context.Background(), pickup, destination, entities.GeoPoint{Lat: -8.9, Lng: 13.1})

	require.NoError(t, err)
	assert.Equal(t, 8000, route.DistanceMeters)
	assert.Equal(t, 25*time.Minute, route.Duration)
	assert.Equal(t, "abc", route.Geometry)
	assert.Equal(t, services.RouteSourceMapbox, route.Source)
}

func TestRouteFailuresAreRoutingUnavailableWithoutLeakingTheToken(t *testing.T) {
	t.Run("no route", func(t *testing.T) {
		client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "NoRoute", "routes": []any{}})
		})
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
	})
	t.Run("server error", func(t *testing.T) {
		client := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
	})
	t.Run("unreachable", func(t *testing.T) {
		client := mapbox.NewClient("secret-token", "http://127.0.0.1:1", 100*time.Millisecond)
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
		assert.False(t, strings.Contains(err.Error(), "secret-token"))
	})
}

func TestETAsAsksOneDestinationForEveryOrigin(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/directions-matrix/v1/mapbox/driving/")
		assert.Equal(t, "0;1", r.URL.Query().Get("sources"))
		assert.Equal(t, "2", r.URL.Query().Get("destinations"))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "durations": [][]float64{{60}, {120}}})
	})

	etas, err := client.ETAs(context.Background(), []entities.GeoPoint{pickup, destination}, pickup)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Minute, 2 * time.Minute}, etas)
}
