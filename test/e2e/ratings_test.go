//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func complimentOf(t *testing.T, customer *person, category, code string) string {
	t.Helper()
	options := customer.get(t, "/api/v1/rating-options?category="+category)
	require.Equal(t, http.StatusOK, options.status, options.text)
	for _, item := range options.body["compliments"].([]any) {
		compliment := item.(map[string]any)
		if compliment["code"] == code {
			return compliment["id"].(string)
		}
	}
	require.Failf(t, "unknown compliment", "%s/%s", category, code)
	return ""
}

func TestAServiceHasItsOwnComplimentsAndTips(t *testing.T) {
	customer := newCustomer(t)
	towing := customer.get(t, "/api/v1/rating-options?category=towing")
	require.Equal(t, http.StatusOK, towing.status, towing.text)
	labels := []string{}
	for _, item := range towing.body["compliments"].([]any) {
		labels = append(labels, item.(map[string]any)["label"].(string))
	}
	assert.Equal(t, []string{"Chegou rápido", "Cuidadoso", "Profissional", "Boa comunicação"}, labels)
	tips := []float64{}
	for _, item := range towing.body["tips"].([]any) {
		tips = append(tips, item.(map[string]any)["amount_cents"].(float64))
	}
	assert.Equal(t, []float64{50_000, 100_000, 200_000}, tips)

	removals := customer.get(t, "/api/v1/rating-options?category=removals")
	first := removals.body["tips"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 200_000, first["amount_cents"])
}

func TestARatingIsOnceOnlyOnACompletedRequestAndCountsForTheProvider(t *testing.T) {
	customer, driver, id, _ := completedTaxi(t, "cash")
	path := "/api/v1/requests/" + id + "/rating"
	view := customer.get(t, "/api/v1/requests/"+id)
	assert.Equal(t, true, view.body["can_rate"])
	assert.Zero(t, view.body["provider"].(map[string]any)["rating_count"])
	assert.EqualValues(t, 1, view.body["provider"].(map[string]any)["completed_requests"])

	intruder := newCustomer(t)
	assert.Equal(t, http.StatusNotFound, intruder.post(t, path, map[string]any{"stars": 5}).status, "only the customer of the request")
	assert.Equal(t, http.StatusForbidden, driver.post(t, path, map[string]any{"stars": 5}).status, "not the driver")
	for _, stars := range []int{0, 6, -1, 99_999_999_999} {
		assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"stars": stars}).status, "%d stars", stars)
	}
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{}).status, "the stars are mandatory")
	assert.Equal(t, "COMPLIMENT_NOT_VALID", customer.post(t, path, map[string]any{"stars": 5, "compliment_ids": []string{uuid.NewString()}}).code())
	assert.Equal(t, "COMPLIMENT_NOT_VALID", customer.post(t, path, map[string]any{"stars": 5, "compliment_ids": []string{complimentOf(t, customer, "towing", "fast")}}).code(), "a compliment of another service")

	compliment := complimentOf(t, customer, "taxi", "clean_car")
	var wg sync.WaitGroup
	statuses := make([]int, 3)
	for i := range statuses {
		wg.Go(func() {
			statuses[i] = customer.post(t, path, map[string]any{"stars": 4, "compliment_ids": []string{compliment}, "comment": "Boa viagem"}).status
		})
	}
	wg.Wait()
	created := 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			created++
		}
	}
	assert.Equal(t, 1, created, "three clicks, one rating: %v", statuses)
	assert.Equal(t, "ALREADY_RATED", customer.post(t, path, map[string]any{"stars": 1}).code())

	view = customer.get(t, "/api/v1/requests/"+id)
	assert.Equal(t, false, view.body["can_rate"])
	given := view.body["rating"].(map[string]any)
	assert.EqualValues(t, 4, given["stars"])
	assert.Equal(t, "Boa viagem", given["comment"])
	provider := view.body["provider"].(map[string]any)
	assert.EqualValues(t, 4, provider["rating"])
	assert.EqualValues(t, 1, provider["rating_count"])

	var sum, count int
	require.NoError(t, env.owner.QueryRow(context.Background(), `SELECT rating_sum, rating_count FROM providers WHERE user_id = $1`, driver.id).Scan(&sum, &count))
	assert.Equal(t, 4, sum, "one rating was counted, not three")
	assert.Equal(t, 1, count)

	mine := driver.get(t, "/api/v1/providers/me/ratings")
	require.Equal(t, http.StatusOK, mine.status, mine.text)
	assert.EqualValues(t, 4, mine.body["average"])
	assert.Equal(t, "Viatura limpa", mine.body["items"].([]any)[0].(map[string]any)["compliments"].([]any)[0].(map[string]any)["label"])

	active := requestTaxi(t, intruder)
	assert.Equal(t, "RATING_NOT_ALLOWED", intruder.post(t, "/api/v1/requests/"+active.str("id")+"/rating", map[string]any{"stars": 5}).code(), "only a completed request")
}

func TestATipGoesWhollyToTheDriverEvenAfterCash(t *testing.T) {
	customer, driver, id, _ := completedTaxi(t, "cash")
	commission := commissionOf(t, id)
	require.Equal(t, http.StatusOK, driver.post(t, "/api/v1/requests/"+id+"/payments/cash", nil).status)
	assert.EqualValues(t, -commission, driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents"))

	path := "/api/v1/requests/" + id + "/tips"
	intruder := newCustomer(t)
	assert.Equal(t, http.StatusNotFound, intruder.post(t, path, map[string]any{"amount_cents": 50_000}).status)
	assert.Equal(t, http.StatusForbidden, driver.post(t, path, map[string]any{"amount_cents": 50_000}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"amount_cents": 0}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, customer.post(t, path, map[string]any{"amount_cents": 50_000, "method": "cash"}).status, "a tip is digital")
	assert.Equal(t, "TIP_OUT_OF_RANGE", customer.post(t, path, map[string]any{"amount_cents": 100_000_000, "phone_number": "900000000"}).code(), "above the limit in the backoffice")

	tip := customer.post(t, path, map[string]any{"amount_cents": 50_000, "phone_number": "900000000"})
	require.Equal(t, http.StatusCreated, tip.status, tip.text)
	assert.Equal(t, "tip", tip.str("kind"))
	assert.Equal(t, "gpo", tip.str("method"), "the digital method, because the request was paid in cash")
	again := customer.post(t, path, map[string]any{"amount_cents": 50_000, "phone_number": "900000000"})
	assert.Equal(t, http.StatusOK, again.status, "a second click returns the same pending tip")
	assert.Equal(t, tip.str("id"), again.str("id"))

	env.appypay.settle(tip.str("code"), "Success", 0)
	require.Eventually(t, func() bool {
		return driver.get(t, "/api/v1/providers/me/ledger").num("balance_cents") == 50_000-commission
	}, 15*time.Second, 50*time.Millisecond, "no commission on a tip")
	assertLedgerBalanced(t)

	view := customer.get(t, "/api/v1/requests/"+id)
	assert.Equal(t, "paid", view.str("payment_status"), "the tip does not touch the status of the fare")
	assert.EqualValues(t, 50_000, view.body["breakdown"].(map[string]any)["tip_cents"])
	assert.Equal(t, "TIP_ALREADY_GIVEN", customer.post(t, path, map[string]any{"amount_cents": 50_000, "phone_number": "900000000"}).code())

	running := newCustomer(t)
	active := requestTaxi(t, running)
	assert.Equal(t, "TIP_NOT_ALLOWED", running.post(t, "/api/v1/requests/"+active.str("id")+"/tips", map[string]any{"amount_cents": 50_000}).code())
}

func TestTheBackofficeManagesCatalogsAndSettings(t *testing.T) {
	admin := staffToken(t)
	customer := newCustomer(t)
	assert.Equal(t, http.StatusForbidden, customer.get(t, "/api/v1/admin/catalog/compliment").status)
	assert.Equal(t, http.StatusForbidden, customer.do(t, http.MethodPut, "/api/v1/admin/settings/max_tip_cents", map[string]any{"value": "1"}, nil).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/catalog/nada", map[string]any{"code": "abc", "label": "x"}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/catalog/compliment", map[string]any{"code": "abc", "label": "x", "amount_cents": -1}).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.post(t, "/api/v1/admin/catalog/compliment", map[string]any{"code": "abc", "label": "x", "display_order": 99_999_999_999}).status)

	created := admin.post(t, "/api/v1/admin/catalog/tip_preset", map[string]any{"code": "tip_novo", "label": "Novo", "amount_cents": 77_700})
	require.Equal(t, http.StatusCreated, created.status, created.text)
	assert.Equal(t, http.StatusConflict, admin.post(t, "/api/v1/admin/catalog/tip_preset", map[string]any{"code": "tip_novo", "label": "Novo", "amount_cents": 1}).status)
	assert.Contains(t, customer.get(t, "/api/v1/rating-options?category=taxi").text, "77700", "it reaches the app")
	updated := admin.do(t, http.MethodPut, "/api/v1/admin/catalog/tip_preset/"+created.str("id"), map[string]any{"code": "tip_novo", "label": "Novo", "amount_cents": 77_700, "is_active": false}, nil)
	require.Equal(t, http.StatusOK, updated.status, updated.text)
	assert.NotContains(t, customer.get(t, "/api/v1/rating-options?category=taxi").text, "77700", "inactive leaves the app")
	assert.Equal(t, http.StatusNotFound, admin.do(t, http.MethodPut, "/api/v1/admin/catalog/compliment/"+created.str("id"), map[string]any{"code": "x1", "label": "x"}, nil).status, "the item of another catalog")

	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, "/api/v1/admin/settings/unknown_key", map[string]any{"value": "1"}, nil).status)
	assert.Equal(t, http.StatusUnprocessableEntity, admin.do(t, http.MethodPut, "/api/v1/admin/settings/max_tip_cents", map[string]any{"value": "muitos"}, nil).status)
	changed := admin.do(t, http.MethodPut, "/api/v1/admin/settings/max_tip_cents", map[string]any{"value": "6000000"}, nil)
	require.Equal(t, http.StatusOK, changed.status, changed.text)
	assert.EqualValues(t, 6_000_000, customer.get(t, "/api/v1/rating-options?category=taxi").num("max_tip_cents"))
	admin.do(t, http.MethodPut, "/api/v1/admin/settings/max_tip_cents", map[string]any{"value": "5000000"}, nil)
}
