package googlemaps_test

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
	"github.com/Agora-Mobility/atlas/infra/geo/googlemaps"
)

var (
	pickup      = entities.GeoPoint{Lat: -8.84, Lng: 13.23}
	destination = entities.GeoPoint{Lat: -8.92, Lng: 13.18}
	stop        = entities.GeoPoint{Lat: -8.9, Lng: 13.1}
)

func serve(t *testing.T, handler http.HandlerFunc) *googlemaps.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return googlemaps.NewClient("key.test", server.URL, time.Second)
}

func answer(elements ...map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(elements) }
}

func TestRouteAsksForTrafficAndReadsDistanceDurationAndPolyline(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/directions/v2:computeRoutes", r.URL.Path)
		assert.Equal(t, "key.test", r.Header.Get("X-Goog-Api-Key"))
		assert.Empty(t, r.URL.RawQuery, "the key never travels in the URL")
		assert.Equal(t, "routes.distanceMeters,routes.duration,routes.polyline.encodedPolyline", r.Header.Get("X-Goog-FieldMask"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "DRIVE", body["travelMode"])
		assert.Equal(t, "TRAFFIC_AWARE", body["routingPreference"])
		assert.Equal(t, "OVERVIEW", body["polylineQuality"])
		assert.Equal(t, map[string]any{"location": map[string]any{"latLng": map[string]any{"latitude": -8.84, "longitude": 13.23}}}, body["origin"])
		assert.Equal(t, map[string]any{"location": map[string]any{"latLng": map[string]any{"latitude": -8.92, "longitude": 13.18}}}, body["destination"])
		assert.Equal(t, []any{map[string]any{"location": map[string]any{"latLng": map[string]any{"latitude": -8.9, "longitude": 13.1}}}}, body["intermediates"])

		_ = json.NewEncoder(w).Encode(map[string]any{"routes": []map[string]any{{
			"distanceMeters": 8000, "duration": "1500s", "polyline": map[string]any{"encodedPolyline": "abc"},
		}}})
	})

	route, err := client.Route(context.Background(), pickup, destination, stop)

	require.NoError(t, err)
	assert.Equal(t, 8000, route.DistanceMeters)
	assert.Equal(t, 25*time.Minute, route.Duration)
	assert.Equal(t, "abc", route.Geometry)
	assert.Equal(t, services.RouteSourceGoogle, route.Source)
}

func TestRouteWithoutStopsSendsNoIntermediates(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.NotContains(t, body, "intermediates")
		_ = json.NewEncoder(w).Encode(map[string]any{"routes": []map[string]any{{"distanceMeters": 1, "duration": "1s"}}})
	})

	_, err := client.Route(context.Background(), pickup, destination)

	require.NoError(t, err)
}

func TestRouteFailuresAreRoutingUnavailableWithoutLeakingTheKey(t *testing.T) {
	t.Run("no route", func(t *testing.T) {
		client := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
	})
	t.Run("server error", func(t *testing.T) {
		client := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
	})
	t.Run("invalid duration", func(t *testing.T) {
		client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"routes": []map[string]any{{"distanceMeters": 1}}})
		})
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
	})
	t.Run("unreachable", func(t *testing.T) {
		client := googlemaps.NewClient("secret-key", "http://127.0.0.1:1", 100*time.Millisecond)
		_, err := client.Route(context.Background(), pickup, destination)
		require.ErrorIs(t, err, services.ErrRoutingUnavailable)
		assert.False(t, strings.Contains(err.Error(), "secret-key"))
	})
}

func TestETAsAsksOneDestinationForEveryOriginAndKeepsTheirOrder(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/distanceMatrix/v2:computeRouteMatrix", r.URL.Path)
		assert.Equal(t, "key.test", r.Header.Get("X-Goog-Api-Key"))
		assert.Contains(t, r.Header.Get("X-Goog-FieldMask"), "status")

		var body struct {
			Origins           []any  `json:"origins"`
			Destinations      []any  `json:"destinations"`
			RoutingPreference string `json:"routingPreference"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Len(t, body.Origins, 2)
		assert.Len(t, body.Destinations, 1)
		assert.Equal(t, "TRAFFIC_UNAWARE", body.RoutingPreference)

		// Google does not promise any order, and may leave a zero index out.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"originIndex": 1, "status": map[string]any{}, "condition": "ROUTE_EXISTS", "duration": "120s"},
			{"status": map[string]any{}, "condition": "ROUTE_EXISTS", "duration": "60s"},
		})
	})

	etas, err := client.ETAs(context.Background(), []entities.GeoPoint{pickup, destination}, stop)

	require.NoError(t, err)
	assert.Equal(t, []time.Duration{time.Minute, 2 * time.Minute}, etas)
}

func TestETAsWithoutOriginsCallsNobody(t *testing.T) {
	client := serve(t, func(http.ResponseWriter, *http.Request) { t.Error("no call expected") })

	etas, err := client.ETAs(context.Background(), nil, pickup)

	require.NoError(t, err)
	assert.Empty(t, etas)
}

func TestETAsFailsWhenAnyOriginHasNoAnswer(t *testing.T) {
	origins := []entities.GeoPoint{pickup, destination}
	ok := map[string]any{"originIndex": 0, "condition": "ROUTE_EXISTS", "duration": "60s"}
	cases := map[string]http.HandlerFunc{
		"route not found": answer(ok, map[string]any{"originIndex": 1, "condition": "ROUTE_NOT_FOUND"}),
		"element error":   answer(ok, map[string]any{"originIndex": 1, "status": map[string]any{"code": 3, "message": "bad"}}),
		"missing origin":  answer(ok),
		"unknown origin":  answer(ok, map[string]any{"originIndex": 7, "condition": "ROUTE_EXISTS", "duration": "60s"}),
		"no duration":     answer(ok, map[string]any{"originIndex": 1, "condition": "ROUTE_EXISTS"}),
		"http error":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := serve(t, handler).ETAs(context.Background(), origins, stop)
			require.ErrorIs(t, err, services.ErrRoutingUnavailable)
		})
	}
}
