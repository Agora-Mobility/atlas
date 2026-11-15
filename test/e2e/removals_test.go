//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func removalWindow(t *testing.T, code string) (string, time.Time) {
	t.Helper()
	var id string
	var minute int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT id, (attrs->>'start_minute')::int FROM catalog_items WHERE kind = 'removal_window' AND code = $1`, code).Scan(&id, &minute))
	luanda := time.FixedZone("WAT", 3600)
	day := time.Now().In(luanda).AddDate(0, 0, 10)
	return id, time.Date(day.Year(), day.Month(), day.Day(), 0, minute, 0, 0, luanda)
}

func removalEstimate(t *testing.T, customer *person, when time.Time, vehicle string, removal map[string]any) map[string]any {
	t.Helper()
	res := customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "removals", "pickup": pickup, "destination": target, "scheduled_for": when, "removal": removal})
	require.Equal(t, http.StatusCreated, res.status, res.text)
	for _, item := range res.body["items"].([]any) {
		option := item.(map[string]any)
		if option["vehicle_type"] == vehicle {
			return option
		}
	}
	require.Failf(t, "no option", "%s in %s", vehicle, res.text)
	return nil
}

func bookRemoval(t *testing.T, customer *person, estimate map[string]any, windowID string, when time.Time, method string) result {
	t.Helper()
	return customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{
		"service_id": estimate["service_id"], "pickup": pickup, "destination": target, "fare_estimate_id": estimate["fare_estimate_id"], "scheduled_for": when, "payment_method": method,
		"removal": map[string]any{"window_id": windowID, "property_code": "home", "pickup_floor": 2, "destination_floor": 0},
		"details": map[string]any{"volume_m3": 999},
	}, map[string]string{"Idempotency-Key": uuid.NewString()})
}

func TestRemovalPriceAddsVehicleHelpersAndExtrasAndIsClosed(t *testing.T) {
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	estimate := removalEstimate(t, customer, when, "Camião 3,5 t", map[string]any{"helpers": 2, "extras": []string{"packing"}})
	assert.EqualValues(t, 11_000_000, estimate["total_cents"], "85 000 + 2 x 5 000 + 15 000")

	created := bookRemoval(t, customer, estimate, windowID, when, "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.EqualValues(t, 11_000_000, created.num("fare_cents"))
	details := created.body["details"].(map[string]any)
	assert.Nil(t, details["volume_m3"], "the volume of the app is ignored")
	assert.Equal(t, "Casa", details["removal"].(map[string]any)["property"])
	assert.Equal(t, "ROUTE_CHANGE_FORBIDDEN", customer.post(t, "/api/v1/requests/"+created.str("id")+"/route", map[string]any{"destination": pickup}).code(), "the closed price does not follow the route")

	again := bookRemoval(t, customer, estimate, windowID, when, "gpo")
	assert.Equal(t, "FARE_ESTIMATE_USED", again.code(), "an estimate serves one request")
}

func TestRemovalRepeatCreatesNewRequest(t *testing.T) {
	customer := newCustomer(t)
	windowID, when := removalWindow(t, "morning")
	estimate := removalEstimate(t, customer, when, "Camião 3,5 t", map[string]any{"helpers": 2, "extras": []string{"packing"}})
	created := bookRemoval(t, customer, estimate, windowID, when, "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	require.Equal(t, http.StatusOK, cancelAs(t, customer, "customer", created.str("id")).status, "cancel the scheduled request first")

	repeat := customer.post(t, "/api/v1/requests/"+created.str("id")+"/repeat", map[string]any{"scheduled_for": when.AddDate(0, 0, 1)})
	require.Equal(t, http.StatusCreated, repeat.status, repeat.text)
	request, _ := repeat.body["request"].(map[string]any)
	require.NotNil(t, request)
	assert.NotEqual(t, created.str("id"), request["id"])
	details := request["details"].(map[string]any)["removal"].(map[string]any)
	assert.Equal(t, "Casa", details["property"])
	assert.Equal(t, created.num("fare_cents"), int64(request["fare_cents"].(float64)))
}

func TestRemovalRefusesWhatTheCatalogDoesNotHave(t *testing.T) {
	customer := newCustomer(t)
	post := func(removal map[string]any) result {
		return customer.post(t, "/api/v1/fare-estimates", map[string]any{"category": "removals", "pickup": pickup, "destination": target, "removal": removal})
	}
	assert.Equal(t, "REMOVAL_ITEM_NOT_FOUND", post(map[string]any{"items": []map[string]any{{"item_id": uuid.NewString(), "qty": 1}}}).code())
	assert.Equal(t, "REMOVAL_EXTRA_NOT_FOUND", post(map[string]any{"extras": []string{"ouro"}}).code())
	assert.Equal(t, "REMOVAL_TOO_MANY_HELPERS", post(map[string]any{"helpers": 7}).code())
	assert.Equal(t, http.StatusUnprocessableEntity, post(map[string]any{"helpers": 99_999_999_999}).status, "over the limit of the binding")

	windowID, when := removalWindow(t, "morning")
	estimate := removalEstimate(t, customer, when, "Carrinha", map[string]any{})
	refused := customer.do(t, http.MethodPost, "/api/v1/requests", map[string]any{"service_id": estimate["service_id"], "pickup": pickup, "destination": target, "fare_estimate_id": estimate["fare_estimate_id"], "scheduled_for": when}, map[string]string{"Idempotency-Key": uuid.NewString()})
	assert.Equal(t, "REMOVAL_DETAILS_REQUIRED", refused.code())
	offWindow := when.Add(30 * time.Minute)
	assert.Equal(t, "REMOVAL_WINDOW_INVALID", bookRemoval(t, customer, removalEstimate(t, customer, offWindow, "Carrinha", map[string]any{}), windowID, offWindow, "gpo").code())
}

func TestAFullDayIsRefusedAndFreedByACancellation(t *testing.T) {
	ctx := context.Background()
	windowID, when := removalWindow(t, "afternoon")
	when = when.AddDate(0, 0, 3)
	_, err := env.owner.Exec(ctx, `UPDATE catalog_items SET attrs = jsonb_set(attrs, '{daily_capacity}', '1') WHERE kind = 'removal_vehicle' AND code = 'truck_7t'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := env.owner.Exec(context.Background(), `UPDATE catalog_items SET attrs = jsonb_set(attrs, '{daily_capacity}', '2') WHERE kind = 'removal_vehicle' AND code = 'truck_7t'`)
		require.NoError(t, err)
	})
	first, second := newCustomer(t), newCustomer(t)
	created := bookRemoval(t, first, removalEstimate(t, first, when, "Camião 7 t", map[string]any{}), windowID, when, "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	estimate := removalEstimate(t, second, when, "Camião 7 t", map[string]any{})
	assert.Equal(t, "REMOVAL_DAY_FULL", bookRemoval(t, second, estimate, windowID, when, "gpo").code())

	var truck string
	require.NoError(t, env.owner.QueryRow(ctx, `SELECT id FROM catalog_items WHERE kind = 'removal_vehicle' AND code = 'truck_7t'`).Scan(&truck))
	calendar := second.get(t, "/api/v1/removals/availability?vehicle_id="+truck+"&from="+when.Format("2006-01-02"))
	assert.Equal(t, "full", calendar.body["days"].([]any)[0].(map[string]any)["status"])
	assert.Equal(t, "REMOVAL_VEHICLE_NOT_FOUND", second.get(t, "/api/v1/removals/availability?vehicle_id="+uuid.NewString()+"&from="+when.Format("2006-01-02")).code())

	require.Equal(t, http.StatusOK, cancelAs(t, first, "customer", created.str("id")).status)
	assert.Equal(t, http.StatusCreated, bookRemoval(t, second, estimate, windowID, when, "gpo").status)
}

func TestATeamIsVerifiedOnlyByThePartner(t *testing.T) {
	owner := newPartner(t)
	customer := newCustomer(t)
	driver, intruder := newApprovedProvider(t, "removals"), newApprovedProvider(t, "removals")
	windowID, when := removalWindow(t, "midday")
	created := bookRemoval(t, customer, removalEstimate(t, customer, when, "Carrinha", map[string]any{}), windowID, when, "gpo")
	require.Equal(t, http.StatusCreated, created.status, created.text)
	id := created.str("id")
	_, err := env.owner.Exec(context.Background(), `UPDATE service_requests SET provider_id = $1, organization_id = $2, status = 'accepted', next_action_at = NULL WHERE id = $3`, driver.id, owner.organizationID, id)
	require.NoError(t, err)
	members := map[string]any{"members": []map[string]any{{"name": "Paulo", "role": "lead", "verified": true}}}
	mine := driver.do(t, http.MethodPut, "/api/v1/requests/"+id+"/team", members, nil)
	require.Equal(t, http.StatusOK, mine.status, mine.text)
	assert.Equal(t, false, mine.body["items"].([]any)[0].(map[string]any)["verified"], "a driver cannot verify")
	assert.Equal(t, http.StatusNotFound, intruder.do(t, http.MethodPut, "/api/v1/requests/"+id+"/team", members, nil).status)
	assert.Equal(t, http.StatusNotFound, newPartner(t).do(t, http.MethodPut, "/api/v1/fleet/requests/"+id+"/team", members, nil).status, "another partner")
	theirs := owner.do(t, http.MethodPut, "/api/v1/fleet/requests/"+id+"/team", members, nil)
	require.Equal(t, http.StatusOK, theirs.status, theirs.text)
	assert.Equal(t, true, theirs.body["items"].([]any)[0].(map[string]any)["verified"])
	assert.Len(t, customer.get(t, "/api/v1/requests/"+id).body["team"], 1)
}

func removalItemCodes(t *testing.T, res result) []string {
	t.Helper()
	require.Equal(t, http.StatusOK, res.status, res.text)
	codes := []string{}
	for _, raw := range res.body["items"].([]any) {
		codes = append(codes, raw.(map[string]any)["code"].(string))
	}
	return codes
}

func TestRemovalItemsAreFilteredByTheTypeOfProperty(t *testing.T) {
	customer := newCustomer(t)
	every := removalItemCodes(t, customer.get(t, "/api/v1/removals/options"))
	home := removalItemCodes(t, customer.get(t, "/api/v1/removals/options?property=home"))
	office := removalItemCodes(t, customer.get(t, "/api/v1/removals/options?property=office"))
	assert.ElementsMatch(t, every, home, "every item fits a home")
	assert.Contains(t, office, "desk")
	assert.Contains(t, office, "box", "an item without properties fits every type")
	assert.NotContains(t, office, "bed", "a bed belongs to a home")
	assert.Equal(t, "REMOVAL_PROPERTY_NOT_FOUND", customer.get(t, "/api/v1/removals/options?property=castelo").code())

	admin := staffToken(t)
	backoffice := removalItemCodes(t, admin.get(t, "/api/v1/admin/catalog/removal_item?property=office"))
	assert.NotContains(t, backoffice, "bed", "the backoffice filters on the server too")
	assert.Contains(t, backoffice, "desk")

	refused := admin.post(t, "/api/v1/admin/catalog/removal_item", map[string]any{"code": "piano", "label": "Piano", "attrs": map[string]any{"volume_dm3": 2000, "properties": []string{"Casa Grande"}}})
	assert.Equal(t, http.StatusUnprocessableEntity, refused.status, refused.text)
}
