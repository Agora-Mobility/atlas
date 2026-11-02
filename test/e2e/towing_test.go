//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func towingDetails(t *testing.T, customer *person) map[string]any {
	t.Helper()
	var incident string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id FROM catalog_items WHERE kind = 'towing_incident' AND code = 'breakdown'`).Scan(&incident))
	return map[string]any{"incident_id": incident, "drivable": false}
}

func towingOption(t *testing.T, customer *person, code string) map[string]any {
	t.Helper()
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "towing", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	var label string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT label FROM catalog_items WHERE kind = 'towing_vehicle' AND code = $1`, code).Scan(&label))
	for _, item := range res.body["items"].([]any) {
		option := item.(map[string]any)
		if option["vehicle_type"] == label {
			return option
		}
	}
	require.Failf(t, "no option", "%s", code)
	return nil
}

func TestTowingPricesFollowTheVehicleTypeAndIncludedDistance(t *testing.T) {
	admin := staffToken(t)
	customer := newCustomer(t)
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "towing", "pickup": pickup, "destination": target})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	prices := map[string]float64{}
	for _, item := range res.body["items"].([]any) {
		option := item.(map[string]any)
		prices[option["vehicle_type"].(string)] = option["total_cents"].(float64)
		assert.EqualValues(t, 10, option["included_km"])
	}
	assert.Equal(t, map[string]float64{"Ligeiro": 2_500_000, "SUV/Pick-up": 3_200_000, "Motociclo": 1_500_000}, prices, "the 8 km of the route are inside the 10 included")

	var id, attrs string
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id, attrs::text FROM catalog_items WHERE kind = 'towing_vehicle' AND code = 'light'`).Scan(&id, &attrs))
	short := admin.do(t, http.MethodPut, "/api/v1/admin/catalog/towing_vehicle/"+id, map[string]any{"code": "light", "label": "Ligeiro", "amount_cents": 2_500_000, "display_order": 1, "attrs": map[string]any{"included_km": 2, "extra_km_cents": 50_000, "min_capacity_kg": 0}}, nil)
	require.Equal(t, http.StatusOK, short.status, short.text)
	t.Cleanup(func() {
		admin.do(t, http.MethodPut, "/api/v1/admin/catalog/towing_vehicle/"+id, map[string]any{"code": "light", "label": "Ligeiro", "amount_cents": 2_500_000, "display_order": 1, "attrs": map[string]any{"included_km": 10, "extra_km_cents": 50_000, "min_capacity_kg": 0}}, nil)
	})
	assert.EqualValues(t, 2_500_000+6*50_000, towingOption(t, customer, "light")["total_cents"], "6 km above the 2 included")
	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, "/api/v1/admin/catalog/towing_vehicle/"+id, map[string]any{"code": "light", "label": "Ligeiro", "amount_cents": 1, "attrs": map[string]any{"included_km": 99_999_999}}, nil).status, "the attributes are validated")
}

func TestTowingNeedsItsDetailsAndAMatchingVehicle(t *testing.T) {
	clearOnlineProviders(t)
	customer := newCustomer(t)
	option := towingOption(t, customer, "suv_pickup")
	assert.Equal(t, "TOWING_DETAILS_REQUIRED", askFor(t, customer, option, nil).code())
	assert.Equal(t, "INCIDENT_NOT_FOUND", askFor(t, customer, option, map[string]any{"towing": map[string]any{"incident_id": uuid.NewString(), "drivable": true}}).code())

	small := newProvider(t, "towing", point{-8.84, 13.235})
	_, err := env.owner.Exec(context.Background(), `UPDATE vehicles SET capacity_kg = 1000 WHERE provider_id = $1`, small.id)
	require.NoError(t, err)
	big := newProvider(t, "towing", point{-8.841, 13.236})
	_, err = env.owner.Exec(context.Background(), `UPDATE vehicles SET capacity_kg = 5000 WHERE provider_id = $1`, big.id)
	require.NoError(t, err)
	smallStream, bigStream := small.listen(t, ""), big.listen(t, "")

	created := askFor(t, customer, option, map[string]any{"towing": towingDetails(t, customer)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, 3_200_000, created.num("fare_cents"))
	details := created.body["details"].(map[string]any)["towing"].(map[string]any)
	assert.Equal(t, "SUV/Pick-up", details["vehicle_type"])
	assert.Equal(t, false, details["drivable"])
	bigStream.waitFor(t, "request.offer", nil)
	assert.Empty(t, smallStream.ofType("request.offer"), "the truck that cannot carry a SUV gets no offer")
}

func TestTheTowingFormOffersTheNearestPartnerWorkshop(t *testing.T) {
	_, err := env.owner.Exec(context.Background(), `UPDATE workshops SET is_active = false`)
	require.NoError(t, err)
	customer := newCustomer(t)
	owner := newPartner(t)
	near := owner.post(t, "/api/v1/fleet/workshops", map[string]any{"name": "Oficina Perto", "currency": "AOA", "address": map[string]any{"lat": pickup.Lat + 0.001, "lng": pickup.Lng, "line": "Rua A"}})
	require.Equal(t, http.StatusCreated, near.status, near.text)
	far := owner.post(t, "/api/v1/fleet/workshops", map[string]any{"name": "Oficina Longe", "currency": "AOA", "address": map[string]any{"lat": pickup.Lat + 0.2, "lng": pickup.Lng, "line": "Rua B"}})
	require.Equal(t, http.StatusCreated, far.status, far.text)

	options := customer.get(t, fmt.Sprintf("/api/v1/towing/options?lat=%f&lng=%f", pickup.Lat, pickup.Lng))
	require.Equal(t, http.StatusOK, options.status, options.text)
	recommended := options.body["recommended_workshop"].(map[string]any)
	assert.Equal(t, near.str("id"), recommended["id"])
	assert.Less(t, recommended["distance_m"], 500.0)
	assert.Len(t, options.body["vehicle_types"], 3)
	assert.NotEmpty(t, options.body["incidents"])
	assert.Equal(t, http.StatusUnprocessableEntity, customer.get(t, "/api/v1/towing/options?lat=999&lng=13").status)
	require.Less(t, owner.do(t, http.MethodDelete, "/api/v1/fleet/workshops/"+near.str("id"), nil, nil).status, 300)
	assert.Equal(t, far.str("id"), customer.get(t, fmt.Sprintf("/api/v1/towing/options?lat=%f&lng=%f", pickup.Lat, pickup.Lng)).body["recommended_workshop"].(map[string]any)["id"], "an inactive workshop is not recommended")
}

func TestTowingEndsOnlyWithTheCodeOfTheCustomer(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "towing", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := askFor(t, customer, towingOption(t, customer, "light"), map[string]any{"towing": towingDetails(t, customer)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, code := created.str("id"), created.str("completion_code")
	assert.Len(t, code, 4)
	offer := stream.waitFor(t, "request.offer", nil)
	assert.NotContains(t, offer.Raw, code, "the driver does not see the code in the offer")
	accepted := acceptOffer(t, driver, offer)
	require.Equal(t, http.StatusOK, accepted.status)
	assert.NotContains(t, accepted.text, `"completion_code"`, "nor in the request")
	for _, step := range []string{"en-route", "arrive", "start"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status)
	}
	path := "/api/v1/requests/" + id + "/complete"
	wrong := "0000"
	if code == wrong {
		wrong = "1111"
	}
	assert.Equal(t, "COMPLETION_CODE_WRONG", driver.post(t, path, map[string]any{"code": wrong}).code())
	assert.Equal(t, "COMPLETION_CODE_WRONG", driver.post(t, path, nil).code(), "without a code")
	assert.Equal(t, "in_progress", customer.get(t, "/api/v1/requests/"+id).str("status"))
	done := driver.post(t, path, map[string]any{"code": code})
	require.Equal(t, http.StatusOK, done.status, done.text)
	assert.Equal(t, "completed", done.str("status"))
}

func TestTheCodeLocksAfterTooManyWrongAttempts(t *testing.T) {
	clearOnlineProviders(t)
	driver := newProvider(t, "towing", point{-8.84, 13.235})
	stream := driver.listen(t, "")
	customer := newCustomer(t)
	created := askFor(t, customer, towingOption(t, customer, "light"), map[string]any{"towing": towingDetails(t, customer)})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id, code := created.str("id"), created.str("completion_code")
	require.Equal(t, http.StatusOK, acceptOffer(t, driver, stream.waitFor(t, "request.offer", nil)).status)
	for _, step := range []string{"en-route", "arrive", "start"} {
		require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/"+step, nil).status)
	}
	wrong := "0000"
	if code == wrong {
		wrong = "1111"
	}
	path := "/api/v1/requests/" + id + "/complete"
	for range 5 {
		require.Equal(t, "COMPLETION_CODE_WRONG", driver.post(t, path, map[string]any{"code": wrong}).code())
	}
	assert.Equal(t, "COMPLETION_CODE_LOCKED", driver.post(t, path, map[string]any{"code": code}).code(), "no guessing 4 digits")
}
